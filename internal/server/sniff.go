package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// D-12/D-13: панель слушает единственный порт, который отвечает только по
// TLS. sniffListener подсматривает первый байт каждого соединения, чтобы
// отличить TLS ClientHello (0x16, RFC 5246 §6.2.1) от обычного HTTP-запроса,
// и либо отдаёт его дальше как есть (TLS), либо сам отвечает 308-редиректом
// на https, не пропуская тело запроса ни в один обработчик.
const (
	tlsHandshakeByte     = 0x16
	sniffPeekTimeout     = 5 * time.Second
	maxPlainRequestBytes = 8 << 10
)

// hostnamePortPattern/ipv6HostPattern — допустимый заголовок Host: имя/IPv4
// (опционально с портом) или IPv6 в скобках (опционально с портом). Иначе —
// подмена на адрес сокета, чтобы заголовок Host не мог инжектировать
// произвольный текст в Location (T-134-23).
var (
	hostnamePortPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
	ipv6HostPattern     = regexp.MustCompile(`^\[[0-9A-Fa-f:]+\](:[0-9]{1,5})?$`)
)

// sniffListener оборачивает обычный net.Listener: цикл Accept() никогда не
// блокируется чтением клиента, классификация каждого соединения (TLS или
// обычный HTTP) идёт в отдельной горутине с дедлайном sniffPeekTimeout, что
// не даёт молчащему соединению (slowloris) заблокировать приём TLS-клиентов
// (T-134-22).
type sniffListener struct {
	inner       net.Listener
	conns       chan net.Conn
	done        chan struct{}
	closeOnce   sync.Once
	peekTimeout time.Duration
	port        string
}

func newSniffListener(inner net.Listener, peekTimeout time.Duration) *sniffListener {
	l := &sniffListener{
		inner:       inner,
		conns:       make(chan net.Conn),
		done:        make(chan struct{}),
		peekTimeout: peekTimeout,
		port:        portFromAddr(inner.Addr()),
	}
	go l.acceptLoop()
	return l
}

func portFromAddr(addr net.Addr) string {
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return port
}

// acceptLoop только принимает соединения и тут же передаёт их на
// классификацию в отдельной горутине — сам он никогда не читает у клиента,
// поэтому не может зависнуть на молчащем соединении.
func (l *sniffListener) acceptLoop() {
	for {
		conn, err := l.inner.Accept()
		if err != nil {
			select {
			case <-l.done:
				return
			default:
				// Внутренний listener больше не может принимать соединения
				// (закрыт снаружи или получил невосстановимую ошибку) —
				// завершаем цикл, как это делает обычный net.Listener.
				return
			}
		}
		go l.classify(conn)
	}
}

// classify подсматривает первый байт соединения с дедлайном peekTimeout,
// снимает дедлайн и либо передаёт TLS-соединение в Accept(), либо сам
// отвечает 308-редиректом на обычный HTTP и закрывает соединение.
func (l *sniffListener) classify(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(l.peekTimeout))
	var first [1]byte
	n, err := io.ReadFull(conn, first[:])
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		_ = conn.Close()
		return
	}

	if first[0] == tlsHandshakeByte {
		peeked := &peekedConn{Conn: conn, prefix: first[:]}
		select {
		case l.conns <- peeked:
		case <-l.done:
			_ = conn.Close()
		}
		return
	}

	redirectPlainHTTP(conn, first[0], l.port)
}

func (l *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *sniffListener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		close(l.done)
		err = l.inner.Close()
	})
	return err
}

func (l *sniffListener) Addr() net.Addr {
	return l.inner.Addr()
}

// peekedConn отдаёт уже прочитанный при классификации байт первым же
// Read(), а дальше прозрачно делегирует оригинальному соединению — ни
// tls.Listener, ни http.Server не замечают, что первый байт был подсмотрен
// заранее.
type peekedConn struct {
	net.Conn
	prefix []byte
	read   int
}

func (c *peekedConn) Read(b []byte) (int, error) {
	if c.read < len(c.prefix) {
		n := copy(b, c.prefix[c.read:])
		c.read += n
		return n, nil
	}
	return c.Conn.Read(b)
}

// redirectPlainHTTP реализует D-13: соединение, начавшееся не с TLS
// handshake, никогда не доходит до http.Server (тело не читается и не
// передаётся ни в один обработчик, T-134-21). Разбирается только
// request-line и заголовок Host, затем пишется 308 на https с тем же Host
// и путём.
func redirectPlainHTTP(c net.Conn, first byte, panelPort string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(sniffPeekTimeout))

	reader := bufio.NewReader(io.MultiReader(
		bytes.NewReader([]byte{first}),
		io.LimitReader(c, maxPlainRequestBytes),
	))

	requestLine, err := reader.ReadString('\n')
	if err != nil {
		writeBadRequest(c)
		return
	}
	parts := strings.Fields(requestLine)
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "HTTP/") {
		writeBadRequest(c)
		return
	}

	target := parts[1]
	if !strings.HasPrefix(target, "/") {
		target = "/"
	}

	host := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			if strings.EqualFold(key, "Host") {
				host = strings.TrimSpace(line[idx+1:])
			}
		}
	}

	host = normalizeHost(host, c.LocalAddr(), panelPort)

	location := fmt.Sprintf("https://%s%s", host, target)
	resp := "HTTP/1.1 308 Permanent Redirect\r\n" +
		"Location: " + location + "\r\n" +
		"Content-Length: 0\r\n" +
		"Connection: close\r\n\r\n"
	_, _ = c.Write([]byte(resp))
}

func writeBadRequest(c net.Conn) {
	_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"))
}

// normalizeHost проверяет заголовок Host допустимым паттерном (T-134-23:
// исключает CR/LF-инъекцию и произвольный текст в Location) и дополняет его
// портом панели, если порт не указан. Недопустимый или отсутствующий Host
// заменяется адресом сокета, на который реально пришло соединение.
func normalizeHost(host string, local net.Addr, panelPort string) string {
	if host == "" || !isValidHost(host) {
		return local.String()
	}
	if strings.HasPrefix(host, "[") {
		if strings.Contains(host, "]:") {
			return host
		}
		return host + ":" + panelPort
	}
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":" + panelPort
}

func isValidHost(host string) bool {
	return hostnamePortPattern.MatchString(host) || ipv6HostPattern.MatchString(host)
}
