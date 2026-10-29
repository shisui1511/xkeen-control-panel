package geodata

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Уровни результата проверки.
const (
	ProbeLevelOK    = "ok"
	ProbeLevelWarn  = "warn"
	ProbeLevelBlock = "block"
)

// Пути, которым проверка дошла до адреса (поле Route).
const (
	ProbeRouteDirect  = "direct"
	ProbeRouteGhProxy = "gh_proxy"
	ProbeRouteMirror  = "mirror"
)

// Режимы проверки: имя проверяется при добавлении, при правке URL — нет.
const (
	ProbeModeAdd  = "add"
	ProbeModeEdit = "edit"
)

const (
	probePrefixBytes    = 8192
	probeHTMLBytes      = 100
	probeURLMax         = 2048
	probeNameMax        = 255
	probeAttemptTimeout = 15 * time.Second
	probeConnectTimeout = 10 * time.Second
	probeMaxRedirects   = 10

	// Порог «тяжёлого» файла и запас места задаются здесь и только здесь:
	// предупреждение, если файл не меньше probeHeavyBytes или в памяти он займёт
	// больше доступной ОЗУ (размер × probeRAMFactor). Значения подобраны
	// оценкой, не замером.
	probeHeavyBytes   = 4 << 20
	probeRAMFactor    = 8
	probeSpaceReserve = 1 << 20
)

var (
	probeNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dat$`)
	// Шаблон XKeen для «вместо файла пришла страница»: начало любой строки
	// из первых 100 байт.
	probeHTMLRe = regexp.MustCompile(`(?mi)^(<!doctype|<html|<head|<body|404|error|not found)`)
)

// ProbeInput — вход проверки своего источника. Занятые имена, зеркала и адрес
// GitHub передаёт вызывающий: пакет их не угадывает.
type ProbeInput struct {
	File      string
	URL       string
	GhProxy   string
	Mode      string
	Mirrors   []string
	GitHubURL string
	GeoDir    string
	Taken     map[string]bool
}

// ProbeCheck — одна проверка. Params содержит только коды, размеры и имя файла,
// адреса сюда не попадают.
type ProbeCheck struct {
	Code   string            `json:"code"`
	Level  string            `json:"level"`
	Params map[string]string `json:"params,omitempty"`
}

// ProbeResult — итог проверки. Blocked — есть проверка уровня block. Route —
// путь, которым XKeen пойдёт к адресу: direct, gh_proxy, mirror или пусто.
type ProbeResult struct {
	Checks  []ProbeCheck `json:"checks"`
	Blocked bool         `json:"blocked"`
	Route   string       `json:"route"`
	Size    int64        `json:"size,omitempty"`
	Kind    string       `json:"kind,omitempty"`
}

// CheckName проверяет имя своего источника: только [A-Za-z0-9._-] и расширение
// .dat (строже XKeen: без /, \ и ..), имя не занято стандартными и другими записями.
func CheckName(name string, taken map[string]bool) ProbeCheck {
	if len(name) > probeNameMax || strings.Contains(name, "..") || !probeNameRe.MatchString(name) {
		return ProbeCheck{Code: "name_bad", Level: ProbeLevelBlock}
	}
	if taken[name] {
		return ProbeCheck{Code: "name_taken", Level: ProbeLevelBlock, Params: map[string]string{"file": name}}
	}
	return ProbeCheck{Code: "name_ok", Level: ProbeLevelOK}
}

// probePath — один путь скачивания: прямой, через gh_proxy или зеркало.
type probePath struct {
	route  string
	prefix string
}

// probePaths повторяет порядок XKeen: при заданном gh_proxy только он, иначе
// напрямую, затем зеркала.
func probePaths(ghProxy string, mirrors []string) []probePath {
	if gh := strings.TrimRight(strings.TrimSpace(ghProxy), "/"); gh != "" {
		return []probePath{{route: ProbeRouteGhProxy, prefix: gh}}
	}
	paths := []probePath{{route: ProbeRouteDirect}}
	for _, m := range mirrors {
		if m = strings.TrimRight(strings.TrimSpace(m), "/"); m != "" {
			paths = append(paths, probePath{route: ProbeRouteMirror, prefix: m})
		}
	}
	return paths
}

// target — адрес запроса: префикс без концевого «/» плюс «/» плюс исходный URL.
func (p probePath) target(raw string) string {
	if p.prefix == "" {
		return raw
	}
	return p.prefix + "/" + raw
}

// shellUnsafe — в адресе есть знак, опасный для оболочки: пробел, управляющий символ
// или один из " ' \ ` $ ; | < >. Значение адреса источника и gh_proxy обрабатывают
// скрипты XKeen от root, поэтому одна проверка на оба. & разрешён: это разделитель
// параметров запроса, а XKeen передаёт адрес в кавычках.
func shellUnsafe(s string) bool {
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return true
		}
		switch r {
		case '"', '\'', '\\', '`', '$', ';', '|', '<', '>':
			return true
		}
	}
	return false
}

func probeCheckScheme(raw string) (ProbeCheck, *url.URL) {
	bad := ProbeCheck{Code: "scheme_bad", Level: ProbeLevelBlock}
	if raw == "" || len(raw) > probeURLMax || shellUnsafe(raw) {
		return bad, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return bad, nil
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return ProbeCheck{Code: "scheme_https", Level: ProbeLevelOK}, u
	case "http":
		return ProbeCheck{Code: "scheme_http", Level: ProbeLevelWarn}, u
	}
	return bad, nil
}

func probeIsGitHubHost(host string) bool {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h == "github.com" || strings.HasSuffix(h, ".githubusercontent.com") || h == "githubusercontent.com"
}

// probeClient — клиент без сжатия: сервер с gzip не должен прятать Content-Length.
// Соединения идут через utils.SafeHTTPClient: адрес, введённый пользователем, не может
// вести в частную сеть, на петлю и link-local, в том числе после редиректа.
func probeClient() (*http.Client, func()) {
	tr := &http.Transport{DialContext: (&net.Dialer{Timeout: probeConnectTimeout}).DialContext}
	if !probeAllowPrivate {
		if safe, ok := utils.SafeHTTPClient(probeAttemptTimeout).Transport.(*http.Transport); ok {
			tr = safe
		}
	}
	tr.TLSHandshakeTimeout = probeConnectTimeout
	tr.ResponseHeaderTimeout = probeAttemptTimeout
	tr.DisableCompression = true
	tr.DisableKeepAlives = true
	cl := &http.Client{
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= probeMaxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return cl, tr.CloseIdleConnections
}

// probeAllowPrivate снимает запрет на частные адреса и loopback. Выставляют только
// роутерные тесты пакета: сервер-образец у них поднят на loopback самого роутера.
var probeAllowPrivate bool

func probeDo(ctx context.Context, cl *http.Client, method, target, rng string) (*http.Response, error) {
	if err := utils.ValidateURL(ctx, target, probeAllowPrivate); err != nil {
		return nil, utils.ErrPrivateNetwork
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	return cl.Do(req)
}

// probeReason сводит ошибку сети к коду без адреса.
func probeReason(err error) string {
	var dnsErr *net.DNSError
	var certErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invErr x509.CertificateInvalidError
	var recErr tls.RecordHeaderError
	switch {
	case errors.Is(err, utils.ErrPrivateNetwork):
		return "private"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.As(err, &certErr), errors.As(err, &hostErr), errors.As(err, &invErr), errors.As(err, &recErr):
		return "tls"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "network"
}

func probeStatusReason(code int) string { return "http_" + strconv.Itoa(code) }

// probeSize повторяет определение ожидаемого размера у XKeen: HEAD с редиректами,
// код 2xx и Content-Length больше нуля; при 405 — GET Range 0-0 и Content-Range.
// ok — адрес ответил кодом 2xx (размер может быть неизвестен, тогда 0).
func probeSize(ctx context.Context, cl *http.Client, target string) (size int64, reason string, ok bool) {
	resp, err := probeDo(ctx, cl, http.MethodHead, target, "")
	if err != nil {
		return 0, probeReason(err), false
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return probeRangeSize(ctx, cl, target)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, probeStatusReason(resp.StatusCode), false
	}
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	}
	return size, "", true
}

func probeRangeSize(ctx context.Context, cl *http.Client, target string) (size int64, reason string, ok bool) {
	resp, err := probeDo(ctx, cl, http.MethodGet, target, "bytes=0-0")
	if err != nil {
		return 0, probeReason(err), false
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, probeStatusReason(resp.StatusCode), false
	}
	cr := resp.Header.Get("Content-Range")
	if i := strings.LastIndex(cr, "/"); i >= 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil && n > 0 {
			size = n
		}
	}
	return size, "", true
}

// probePrefix читает не больше 8 КиБ начала файла: сервер без Range отдаёт 200
// с полным телом, но дальше префикса не читается ничего.
func probePrefix(ctx context.Context, cl *http.Client, target string) ([]byte, string, bool) {
	resp, err := probeDo(ctx, cl, http.MethodGet, target, "bytes=0-"+strconv.Itoa(probePrefixBytes-1))
	if err != nil {
		return nil, probeReason(err), false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, probeStatusReason(resp.StatusCode), false
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, probePrefixBytes))
	if err != nil && len(buf) == 0 {
		return nil, probeReason(err), false
	}
	return buf, "", true
}

// probeReach идёт по путям по порядку и возвращает первый, ответивший кодом 2xx.
func probeReach(ctx context.Context, cl *http.Client, paths []probePath, raw string) (found probePath, size int64, reason string, ok bool) {
	for _, p := range paths {
		actx, cancel := context.WithTimeout(ctx, probeAttemptTimeout)
		sz, rs, good := probeSize(actx, cl, p.target(raw))
		cancel()
		if good {
			return p, sz, "", true
		}
		reason = rs
		if ctx.Err() != nil {
			break
		}
	}
	return probePath{}, 0, reason, false
}

// probeMemAvailable читает MemAvailable в байтах; 0 — не удалось.
func probeMemAvailable() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}

// probeFreeSpace возвращает свободные байты каталога; ok=false — не удалось.
func probeFreeSpace(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}

// Probe проверяет свой источник до записи, не скачивая файл целиком: имя, схема,
// сеть тем же путём, что у XKeen, размер, страница вместо файла, вид базы, место
// и память. Тело ответа наружу не отдаётся — только классификация. Адрес, на
// который не ответил ни один путь, даёт предупреждение, но не блок: запись
// разрешена.
func Probe(ctx context.Context, in ProbeInput) ProbeResult {
	res := ProbeResult{Checks: []ProbeCheck{}}
	add := func(c ProbeCheck) {
		res.Checks = append(res.Checks, c)
		if c.Level == ProbeLevelBlock {
			res.Blocked = true
		}
	}

	if in.Mode != ProbeModeEdit {
		add(CheckName(in.File, in.Taken))
	}
	sch, u := probeCheckScheme(in.URL)
	add(sch)
	if u == nil {
		return res
	}
	if strings.TrimSpace(in.GhProxy) != "" && !probeIsGitHubHost(u.Host) {
		add(ProbeCheck{Code: "proxy_all", Level: ProbeLevelWarn})
	}

	paths := probePaths(in.GhProxy, in.Mirrors)
	cl, closeConns := probeClient()
	defer closeConns()

	// Достижимость GitHub (предусловие xkeen -ug) проверяется параллельно.
	var ghDone chan bool
	if in.GitHubURL != "" {
		ghDone = make(chan bool, 1)
		go func() {
			_, _, _, ok := probeReach(ctx, cl, paths, in.GitHubURL)
			ghDone <- ok
		}()
	}
	finishGitHub := func() {
		if ghDone != nil && !<-ghDone {
			add(ProbeCheck{Code: "gh_unreachable", Level: ProbeLevelWarn})
		}
	}

	if len(paths) == 1 && paths[0].route == ProbeRouteGhProxy {
		res.Route = ProbeRouteGhProxy
	}
	found, size, reason, ok := probeReach(ctx, cl, paths, in.URL)
	if !ok {
		add(ProbeCheck{Code: "reach_fail", Level: ProbeLevelWarn, Params: map[string]string{"reason": reason}})
		finishGitHub()
		return res
	}
	res.Route = found.route

	var prefix []byte
	prefixReason := ""
	if size > 0 {
		pctx, cancel := context.WithTimeout(ctx, probeAttemptTimeout)
		var pok bool
		prefix, prefixReason, pok = probePrefix(pctx, cl, found.target(in.URL))
		cancel()
		if pok {
			prefixReason = ""
		}
	}
	if prefixReason != "" {
		add(ProbeCheck{Code: "reach_fail", Level: ProbeLevelWarn, Params: map[string]string{"reason": prefixReason}})
	} else {
		add(ProbeCheck{Code: "reach_ok", Level: ProbeLevelOK})
	}

	switch {
	case size <= 0:
		add(ProbeCheck{Code: "size_none", Level: ProbeLevelBlock})
	case size < MinDatSize:
		add(ProbeCheck{Code: "size_small", Level: ProbeLevelBlock, Params: map[string]string{"size": strconv.FormatInt(size, 10)}})
	default:
		add(ProbeCheck{Code: "size_ok", Level: ProbeLevelOK, Params: map[string]string{"size": strconv.FormatInt(size, 10)}})
	}
	if size > 0 {
		res.Size = size
	}

	if len(prefix) > 0 {
		head := prefix
		if len(head) > probeHTMLBytes {
			head = head[:probeHTMLBytes]
		}
		switch {
		case probeHTMLRe.Match(head):
			add(ProbeCheck{Code: "html", Level: ProbeLevelBlock})
		case size >= MinDatSize:
			kind, err := lists.SniffDat(prefix)
			if err != nil {
				add(ProbeCheck{Code: "not_dat", Level: ProbeLevelBlock})
			} else {
				res.Kind = kind
				add(ProbeCheck{Code: "kind", Level: ProbeLevelOK, Params: map[string]string{"kind": kind}})
			}
		}
	}

	if size > 0 {
		heavy := size >= probeHeavyBytes
		if mem := probeMemAvailable(); mem > 0 && uint64(size)*probeRAMFactor > mem {
			heavy = true
		}
		if heavy {
			add(ProbeCheck{Code: "heavy", Level: ProbeLevelWarn, Params: map[string]string{"size": strconv.FormatInt(size, 10)}})
		}
		if in.GeoDir != "" {
			if free, ok := probeFreeSpace(in.GeoDir); ok {
				need := uint64(size) + probeSpaceReserve
				if free < need {
					add(ProbeCheck{Code: "space", Level: ProbeLevelBlock, Params: map[string]string{
						"need": strconv.FormatUint(need, 10),
						"free": strconv.FormatUint(free, 10),
					}})
				}
			}
		}
	}

	finishGitHub()
	return res
}
