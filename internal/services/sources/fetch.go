package sources

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Запрос подписки: UA, заголовки устройства, пределы.
const (
	// uaXray — UA клиента Xray-типа: по нему провайдеры отдают Xray-JSON или
	// список ссылок, которые панель разбирает нативно.
	uaXray = "Happ/1.0"
	// uaMihomoFallbackVersion — версия в UA Mihomo, когда версия ядра не известна.
	uaMihomoFallbackVersion = "1.19.0"
	// maxFetchBody — предел распакованного тела ответа (T-145-21).
	maxFetchBody = 10 << 20
	// defaultFetchTimeout и slowFetchTimeout — таймаут запроса к провайдеру
	// (A10: MIPS медленнее ARM).
	defaultFetchTimeout = 30 * time.Second
	slowFetchTimeout    = 60 * time.Second
)

// versionRE вычленяет числовую часть версии ядра («v1.19.38» → «1.19.38»).
var versionRE = regexp.MustCompile(`\d+(?:\.\d+)*`)

// mihomoUA — UA Mihomo с версией установленного ядра: по нему провайдеры отдают
// готовый Clash YAML.
func (s *Service) mihomoUA() string {
	v := versionRE.FindString(s.d.KernelVersion(kernelMihomo))
	if v == "" {
		v = uaMihomoFallbackVersion
	}
	return "ClashMeta/" + v + "; mihomo/" + v
}

// userAgent — UA запроса под ядро.
func (s *Service) userAgent(kernel string) string {
	if kernel == kernelMihomo {
		return s.mihomoUA()
	}
	return uaXray
}

// fetchResult — итог запроса под одно ядро. Err != nil — запрос не дал ответа
// провайдера (сеть, HTTP-статус, предел тела); Body и Header заполнены, когда
// ответ получен.
type fetchResult struct {
	Body   []byte
	Header http.Header
	Status int
	Err    *nodes.SourceError
	// Secrets — части адреса запроса (сегменты пути, значения запроса, учётные
	// данные), которые нельзя выводить: ими маскируется диагностика (D-33).
	Secrets []string
}

// newFetchRequest собирает запрос подписки. HWID и заголовки устройства у обоих
// ядер одинаковы: у провайдера занят один слот устройства (D-06). hwidLocked —
// источник не поддерживает HWID: x-hwid не отправляется.
func (s *Service) newFetchRequest(ctx context.Context, rawURL, kernel, hwid string, hwidLocked bool) (*http.Request, *nodes.SourceError) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, nodes.NewError(nodes.CodeInvalidURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nodes.NewError(nodes.CodeInvalidURL)
	}
	req.Header.Set("User-Agent", s.userAgent(kernel))
	req.Header.Set("Accept", "*/*")
	if hwid != "" && !hwidLocked {
		req.Header.Set("x-hwid", hwid)
	}
	model, osName, osVersion := s.d.Device()
	if osName == "" {
		osName = "Linux"
	}
	req.Header.Set("x-device-os", osName)
	if model != "" {
		req.Header.Set("x-device-model", model)
	}
	if osVersion != "" {
		req.Header.Set("x-ver-os", osVersion)
	}
	return req, nil
}

// traceState — что успело произойти с запросом до отказа: по нему «таймаут» после
// успешного DNS отличается от обычного (A6).
type traceState struct {
	mu      sync.Mutex
	dnsDone bool
	dnsErr  error
	gotConn bool
}

func (t *traceState) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: func(i httptrace.DNSDoneInfo) {
			t.mu.Lock()
			t.dnsDone, t.dnsErr = true, i.Err
			t.mu.Unlock()
		},
		GotConn: func(httptrace.GotConnInfo) {
			t.mu.Lock()
			t.gotConn = true
			t.mu.Unlock()
		},
	}
}

// snapshot возвращает копию состояния: connected — соединение установлено;
// dnsOK — адрес получен (DNS без ошибки, либо в URL IP-адрес).
func (t *traceState) snapshot(host string) (connected, dnsOK bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if net.ParseIP(host) != nil {
		return t.gotConn, true
	}
	return t.gotConn, t.dnsDone && t.dnsErr == nil
}

// fetchKernel запрашивает подписку под ядро: GET с UA ядра, HWID и заголовками
// устройства, тело — не больше 10 МиБ распакованного. Сетевые отказы и статусы
// HTTP превращаются в коды D-32; текст *url.Error с полным URL нигде не
// форматируется (Pitfall 5).
func (s *Service) fetchKernel(ctx context.Context, rawURL, kernel, hwid string, hwidLocked bool) fetchResult {
	timeout := s.d.FetchTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var ts traceState
	req, serr := s.newFetchRequest(httptrace.WithClientTrace(ctx, ts.trace()), rawURL, kernel, hwid, hwidLocked)
	if serr != nil {
		return fetchResult{Err: serr}
	}
	secrets := urlSecrets(req.URL)
	client := s.d.Client(timeout)
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		connected, dnsOK := ts.snapshot(req.URL.Hostname())
		return fetchResult{Err: classifyTransportErr(err, req.URL, connected, dnsOK), Secrets: secrets}
	}
	defer resp.Body.Close()

	res := fetchResult{Header: resp.Header, Status: resp.StatusCode, Secrets: secrets}
	if serr := classifyStatus(resp.StatusCode); serr != nil {
		res.Err = serr
		return res
	}
	body, serr := readBody(resp.Body, maxFetchBody)
	if serr != nil {
		res.Err = serr
		return res
	}
	// Ошибка чтения тела (обрыв, таймаут посреди ответа) приходит из readBody как
	// SourceError; транспортные ошибки при чтении классифицируются там же.
	res.Body = body
	return res
}

// readBody читает тело не больше limit байт. Больше — too_large, память не
// растёт сверх предела (io.LimitReader, T-145-21).
func readBody(r io.Reader, limit int64) ([]byte, *nodes.SourceError) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, classifyReadErr(err)
	}
	if int64(len(body)) > limit {
		return nil, nodes.NewError(nodes.CodeTooLarge, "unit", "bytes", "limit", strconv.FormatInt(limit, 10))
	}
	return body, nil
}

// classifyReadErr — отказ при чтении тела: таймаут или обрыв соединения.
func classifyReadErr(err error) *nodes.SourceError {
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return withDetail(nodes.NewError(nodes.CodeTimeout), err, nil)
	}
	return withDetail(nodes.NewError(nodes.CodeServerError), err, nil)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// classifyStatus переводит статус HTTP в код D-32; 2xx — nil.
func classifyStatus(status int) *nodes.SourceError {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status >= 400 && status < 500:
		// 401, 403, 404 и прочие 4xx: нужен HWID или ссылка устарела.
		e := nodes.NewError(nodes.CodeHTTPAuth, "status", strconv.Itoa(status))
		e.HTTPStatus = status
		return e
	default:
		e := nodes.NewError(nodes.CodeServerError, "status", strconv.Itoa(status))
		e.HTTPStatus = status
		return e
	}
}

// classifyTransportErr превращает сетевую ошибку запроса в код D-32. connected —
// соединение с сервером установлено; dnsOK — адрес получен. Исходный URL в ответ
// не попадает: Detail собирается из внутренней ошибки с маскированием (D-33).
func classifyTransportErr(err error, u *url.URL, connected, dnsOK bool) *nodes.SourceError {
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	var (
		dnsErr    *net.DNSError
		certVerif *tls.CertificateVerificationError
		unknownCA x509.UnknownAuthorityError
		certInv   x509.CertificateInvalidError
		hostErr   x509.HostnameError
		recHdr    tls.RecordHeaderError
		alert     tls.AlertError
	)
	switch {
	case errors.Is(err, utils.ErrPrivateNetwork):
		return withDetail(nodes.NewError(nodes.CodeSSRFBlocked), err, u)
	case errors.As(err, &dnsErr):
		name := dnsErr.Name
		if name == "" {
			name = host
		}
		return withDetail(nodes.NewError(nodes.CodeDNSFailed, "host", name), err, u)
	case errors.As(err, &certVerif), errors.As(err, &unknownCA), errors.As(err, &certInv),
		errors.As(err, &hostErr), errors.As(err, &recHdr), errors.As(err, &alert):
		return withDetail(nodes.NewError(nodes.CodeTLSError, "host", host), err, u)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		// Таймаут при установленном DNS и несостоявшемся соединении: домен
		// недоступен напрямую (A6); иначе — обычный таймаут ответа.
		if !connected && dnsOK {
			return withDetail(nodes.NewError(nodes.CodeUnreachableDirect, "host", host), err, u)
		}
		return withDetail(nodes.NewError(nodes.CodeTimeout, "host", host), err, u)
	}
	var op *net.OpError
	if !connected && errors.As(err, &op) && op.Op == "dial" {
		return withDetail(nodes.NewError(nodes.CodeUnreachableDirect, "host", host), err, u)
	}
	if connected {
		return withDetail(nodes.NewError(nodes.CodeServerError, "host", host), err, u)
	}
	return withDetail(nodes.NewError(nodes.CodeUnreachableDirect, "host", host), err, u)
}

// withDetail заполняет Detail маскированным текстом внутренней ошибки. *url.Error
// разворачивается: его Error() содержит полный адрес с токеном подписки.
func withDetail(e *nodes.SourceError, err error, u *url.URL) *nodes.SourceError {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	e.Detail = nodes.MaskText(err.Error(), urlSecrets(u))
	return e
}

// urlSecrets — части URL, которые нельзя выводить: сегменты пути, значения
// запроса и пароль userinfo.
func urlSecrets(u *url.URL) []string {
	if u == nil {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(u.Path, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	for _, vals := range u.Query() {
		out = append(out, vals...)
	}
	if u.User != nil {
		if p, ok := u.User.Password(); ok {
			out = append(out, p)
		}
		out = append(out, u.User.Username())
	}
	return out
}

// statusGroup — группа состояния источника по коду ошибки D-32.
func statusGroup(code string) string {
	switch code {
	case nodes.CodeAllStubs, nodes.CodeHTTPAuth:
		return StatusRejected
	case nodes.CodeDNSFailed, nodes.CodeTimeout, nodes.CodeTLSError, nodes.CodeSSRFBlocked,
		nodes.CodeUnreachableDirect, nodes.CodeServerError:
		return StatusNetwork
	}
	return StatusProviderError
}

// errorStatus — статус источника по ошибке запроса или ответа.
func errorStatus(e *nodes.SourceError) Status {
	st := Status{Code: statusGroup(e.Code), ErrorCode: e.Code, HTTP: e.HTTPStatus}
	if len(e.Params) > 0 {
		st.Params = make(map[string]string, len(e.Params))
		for k, v := range e.Params {
			st.Params[k] = v
		}
	}
	return st
}

// parsed — разобранный ответ провайдера.
type parsed struct {
	Format  string
	Working []nodes.Node
	Total   int
	Stubs   int
	Skipped int
	// Result — полный итог разбора (для отчёта диагностики); пуст, если тело не
	// дошло до разбора (HTML, пустой ответ).
	Result nodes.ParseResult
}

// htmlPrefixes — начала HTML-страниц, которые ParseBody не узнаёт по doctype.
var htmlPrefixes = []string{"<head", "<body", "<script", "<?xml"}

// evaluateBody разбирает тело ответа: HTML и пустой ответ — provider_error,
// заглушки отделяются от рабочих узлов, одни заглушки — all_stubs (D-26).
// Чистая функция: сеть и диск не трогает.
func evaluateBody(body []byte, contentType string) (parsed, *nodes.SourceError) {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml") {
		return parsed{}, nodes.NewError(nodes.CodeHTMLResponse)
	}
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	for _, p := range htmlPrefixes {
		if strings.HasPrefix(head, p) {
			return parsed{}, nodes.NewError(nodes.CodeHTMLResponse)
		}
	}
	res, err := nodes.ParseBody(body)
	if err != nil {
		var se *nodes.SourceError
		if errors.As(err, &se) {
			return parsed{Result: res}, se
		}
		return parsed{Result: res}, nodes.NewError(nodes.CodeUnrecognized)
	}
	working, stubs := nodes.SplitStubs(res.Nodes)
	out := parsed{Format: res.Format, Working: working, Total: len(res.Nodes), Stubs: stubs, Skipped: len(res.Skipped), Result: res}
	if len(working) == 0 {
		switch {
		case stubs > 0:
			return out, nodes.NewError(nodes.CodeAllStubs)
		case len(res.Skipped) > 0:
			return out, nodes.NewError(res.Skipped[0].Code)
		}
		return out, nodes.NewError(nodes.CodeEmptyResponse)
	}
	return out, nil
}

// kernelOutcome — итог запроса и разбора под одно ядро. Ref задан, когда рабочий
// набор записан в снимок; иначе Err объясняет отказ.
type kernelOutcome struct {
	Ref    *Ref
	Err    *nodes.SourceError
	Header http.Header
	Meta   Meta
	// Stubs и Skipped известны и при отказе (для предпросмотра).
	Stubs   int
	Skipped int
	// Nodes — рабочие узлы набора (нужны, пока снимок не записан в Live).
	Nodes []nodes.Node
	// fetch и report — ответ провайдера и итог разбора: из них строится диагностика
	// (writeDiag).
	fetch  fetchResult
	report nodes.ParseResult
	// release снимает удержание снимка «в полёте»; nil — снимка не записывалось.
	// Вызывающий отпускает его после того, как ссылка попала в Live или в токен
	// предпросмотра (или когда результат отброшен).
	release func()
}

// dropBody освобождает тяжёлые поля после записи диагностики: тело ответа (до
// 10 МиБ), итог разбора и рабочие узлы. Кандидат предпросмотра живёт в токене до
// 10 минут, и на mipsle со 128 МБ памяти удержание нескольких тел опасно. Для
// «Добавить» и представления хватает Ref, Meta, Err, Stubs и Skipped.
func (oc *kernelOutcome) dropBody() {
	if oc == nil {
		return
	}
	oc.fetch = fetchResult{}
	oc.report = nodes.ParseResult{}
	oc.Nodes = nil
}

// free отпускает удержанный снимок; безопасно вызывать повторно и на nil.
func (oc *kernelOutcome) free() {
	if oc != nil && oc.release != nil {
		oc.release()
	}
}

// fetchOutcome — запрос под ядро, разбор и запись снимка рабочего набора.
// Ошибка возврата — только сбой записи на диск: сетевые и провайдерские отказы
// уходят в kernelOutcome.Err.
func (s *Service) fetchOutcome(ctx context.Context, rawURL, kernel, hwid string, hwidLocked bool) (*kernelOutcome, error) {
	fr := s.fetchKernel(ctx, rawURL, kernel, hwid, hwidLocked)
	now := s.d.Now()
	oc := &kernelOutcome{Header: fr.Header, Meta: parseMeta(fr.Header, now), fetch: fr}
	if fr.Err != nil {
		oc.Err = fr.Err
		return oc, nil
	}
	p, serr := evaluateBody(fr.Body, fr.Header.Get("Content-Type"))
	oc.Stubs, oc.Skipped = p.Stubs, p.Skipped
	oc.report = p.Result
	if serr != nil {
		oc.Err = serr
		return oc, nil
	}
	// Снимок удержан с момента записи: уборка не удалит его до фиксации в Live.
	hash, release, err := s.blobs.putHeld(p.Working)
	if err != nil {
		return nil, err
	}
	oc.release = release
	oc.Nodes = p.Working
	// Usable — узлов, которые ядро получит: полные дубли в ответе (один узел в
	// нескольких конфигурациях Xray-JSON) схлопываются, как в наборе источника, иначе
	// счётчик предпросмотра («57 для Xray») расходился бы с карточкой («20»).
	oc.Ref = &Ref{Hash: hash, FetchedAt: now, Format: p.Format, Total: p.Total, Usable: usableCount(p.Working), Stubs: p.Stubs, Skipped: p.Skipped}
	return oc, nil
}

// usableCount — число узлов ответа, которые получит ядро: полные дубли схлопнуты
// так же, как в наборе источника (nodes.Uniq).
func usableCount(working []nodes.Node) int {
	keyed, _ := nodes.Uniq("", working)
	return len(keyed)
}

// sourceHWID выбирает HWID запроса: свой HWID источника заменяет общий, источник
// с x-hwid-not-supported идёт без HWID (D-06, D-07).
func (s *Service) sourceHWID(cfg SubscriptionConfig, meta Meta) (hwid string, locked bool) {
	if meta.HWIDLocked {
		return "", true
	}
	if custom := strings.TrimSpace(cfg.HWID); custom != "" {
		return custom, false
	}
	return s.d.HWID(), false
}

// fetchAll запрашивает подписку под каждое из ядер параллельно и ждёт все ответы.
// Оба запроса несут один HWID: у провайдера занят один слот устройства.
func (s *Service) fetchAll(ctx context.Context, rawURL, hwid string, hwidLocked bool, kernels []string) (map[string]*kernelOutcome, error) {
	type item struct {
		kernel string
		oc     *kernelOutcome
		err    error
	}
	results := make(chan item, len(kernels))
	var wg sync.WaitGroup
	for _, k := range kernels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oc, err := s.fetchOutcome(ctx, rawURL, k, hwid, hwidLocked)
			results <- item{kernel: k, oc: oc, err: err}
		}()
	}
	wg.Wait()
	close(results)
	out := make(map[string]*kernelOutcome, len(kernels))
	var firstErr error
	for it := range results {
		if it.err != nil && firstErr == nil {
			firstErr = it.err
		}
		if it.oc != nil {
			out[it.kernel] = it.oc
		}
	}
	if firstErr == nil {
		firstErr = ctx.Err()
	}
	if firstErr != nil {
		// Результат отброшен: удержанные снимки отпускаются.
		for _, oc := range out {
			oc.free()
		}
		return nil, firstErr
	}
	return out, nil
}
