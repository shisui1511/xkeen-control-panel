package dnstakeover

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/idna"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// DNS-проба на golang.org/x/net/dns/dnsmessage: в отличие от net.Resolver она
// отличает ответ с любым RCODE (SERVFAIL — слушатель жив) от тишины, что нужно
// сторожу (D-13) и вердикту пути (D-29, D-30).

const (
	// LoopbackDNS — адрес DNS роутера для проб.
	LoopbackDNS = "127.0.0.1:53"
	// probeAttempt — предел одной попытки (как у SetDNSProxying).
	probeAttempt = 3 * time.Second
	// probePause — пауза между попытками.
	probePause = time.Second
	// probeWindow — окно повторов резолва: ядро после перезапуска поднимает
	// слушатель и туннель не сразу.
	probeWindow = 20 * time.Second
	// maxDNSMessage — размер буфера ответа UDP и предел ответа TCP.
	maxDNSMessage = 65535
)

// ErrNoAnswer — ответа на запрос нет: таймаут или сбой сети (отдельно от RCODE).
var ErrNoAnswer = errors.New("dnstakeover: нет ответа DNS")

// ErrNotResolved — ответ пришёл, но имя не разрешилось (RCODE не NOERROR или нет
// адресов).
var ErrNotResolved = errors.New("dnstakeover: имя не разрешилось")

// Answer — ответ DNS: код, адреса записей запрошенного типа и задержка.
type Answer struct {
	RCode   dnsmessage.RCode
	Addrs   []string
	Latency time.Duration
}

// queryName проверяет имя (configlayer.ValidZone) и переводит его в имя запроса
// в ASCII с точкой в конце.
func queryName(name string) (dnsmessage.Name, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if err := configlayer.ValidZone(name); err != nil {
		return dnsmessage.Name{}, err
	}
	ascii, err := idna.Punycode.ToASCII(strings.ToLower(name))
	if err != nil {
		return dnsmessage.Name{}, err
	}
	return dnsmessage.NewName(ascii + ".")
}

// Query отправляет один запрос имени name типа qtype на server (хост:порт) по
// UDP; ответ с флагом TC повторяется по TCP. Ошибка — только когда ответа нет
// (ErrNoAnswer) или имя неверно; RCODE ответа — в Answer.
func Query(ctx context.Context, server, name string, qtype dnsmessage.Type, timeout time.Duration) (Answer, error) {
	qname, err := queryName(name)
	if err != nil {
		return Answer{}, err
	}
	var idb [2]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return Answer{}, err
	}
	id := binary.BigEndian.Uint16(idb[:])
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: qname, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	packed, err := msg.Pack()
	if err != nil {
		return Answer{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	resp, err := exchangeUDP(ctx, server, packed, id)
	if err == nil && resp.Header.Truncated {
		resp, err = exchangeTCP(ctx, server, packed, id)
	}
	if err != nil {
		return Answer{Latency: time.Since(start)}, fmt.Errorf("%w: %v", ErrNoAnswer, err)
	}
	ans := Answer{RCode: resp.Header.RCode, Latency: time.Since(start), Addrs: []string{}}
	for _, rr := range resp.Answers {
		if rr.Header.Type != qtype {
			continue
		}
		switch b := rr.Body.(type) {
		case *dnsmessage.AResource:
			ans.Addrs = append(ans.Addrs, net.IP(b.A[:]).String())
		case *dnsmessage.AAAAResource:
			ans.Addrs = append(ans.Addrs, net.IP(b.AAAA[:]).String())
		}
	}
	return ans, nil
}

// exchangeUDP шлёт запрос и ждёт ответ с тем же ID (чужие датаграммы
// пропускаются).
func exchangeUDP(ctx context.Context, server string, packed []byte, id uint16) (dnsmessage.Message, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", server)
	if err != nil {
		return dnsmessage.Message{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(packed); err != nil {
		return dnsmessage.Message{}, err
	}
	buf := make([]byte, maxDNSMessage)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return dnsmessage.Message{}, err
		}
		var resp dnsmessage.Message
		if resp.Unpack(buf[:n]) != nil || resp.Header.ID != id || !resp.Header.Response {
			continue
		}
		return resp, nil
	}
}

// exchangeTCP повторяет запрос по TCP (длина сообщения — два байта впереди).
func exchangeTCP(ctx context.Context, server string, packed []byte, id uint16) (dnsmessage.Message, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return dnsmessage.Message{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	frame := make([]byte, 2+len(packed))
	binary.BigEndian.PutUint16(frame, uint16(len(packed)))
	copy(frame[2:], packed)
	if _, err := conn.Write(frame); err != nil {
		return dnsmessage.Message{}, err
	}
	var lb [2]byte
	if _, err := io.ReadFull(conn, lb[:]); err != nil {
		return dnsmessage.Message{}, err
	}
	body := make([]byte, binary.BigEndian.Uint16(lb[:]))
	if _, err := io.ReadFull(conn, body); err != nil {
		return dnsmessage.Message{}, err
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(body); err != nil {
		return dnsmessage.Message{}, err
	}
	if resp.Header.ID != id || !resp.Header.Response {
		return dnsmessage.Message{}, errors.New("ответ TCP не на этот запрос")
	}
	return resp, nil
}

// Alive сообщает, что слушатель server отвечает: любой RCODE, включая SERVFAIL,
// значит «жив» (D-13).
func Alive(ctx context.Context, server, name string) bool {
	_, err := Query(ctx, server, name, dnsmessage.TypeA, probeAttempt)
	return err == nil
}

// Resolve разрешает имя через 127.0.0.1:53 с повторами: попытка до 3 с, пауза
// 1 с, окно до 20 с. Успех — NOERROR и хотя бы один адрес. NXDOMAIN —
// окончательный ответ, повторы не идут. Неудача возвращает последний ответ и
// ErrNoAnswer или ErrNotResolved.
func Resolve(ctx context.Context, name string, qtype dnsmessage.Type) (Answer, error) {
	if _, err := queryName(name); err != nil {
		return Answer{}, err
	}
	deadline := time.Now().Add(probeWindow)
	var (
		last    Answer
		lastErr error
	)
	for {
		ans, err := Query(ctx, LoopbackDNS, name, qtype, probeAttempt)
		last, lastErr = ans, err
		if err == nil {
			if ans.RCode == dnsmessage.RCodeSuccess && len(ans.Addrs) > 0 {
				return ans, nil
			}
			lastErr = fmt.Errorf("%w: %s", ErrNotResolved, rcodeName(ans.RCode))
			if ans.RCode == dnsmessage.RCodeNameError {
				return last, lastErr
			}
		}
		if time.Now().Add(probePause).After(deadline) {
			return last, lastErr
		}
		select {
		case <-ctx.Done():
			return last, fmt.Errorf("%w: %v", ErrNoAnswer, ctx.Err())
		case <-time.After(probePause):
		}
	}
}

// rcodeName — короткое имя RCODE для причины в результате проверки.
func rcodeName(rc dnsmessage.RCode) string {
	switch rc {
	case dnsmessage.RCodeSuccess:
		return "noerror"
	case dnsmessage.RCodeFormatError:
		return "formerr"
	case dnsmessage.RCodeServerFailure:
		return "servfail"
	case dnsmessage.RCodeNameError:
		return "nxdomain"
	case dnsmessage.RCodeNotImplemented:
		return "notimp"
	case dnsmessage.RCodeRefused:
		return "refused"
	}
	return fmt.Sprintf("rcode%d", int(rc))
}
