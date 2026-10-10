package sources

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// maxDiagBody — предел тела ответа в файле диагностики (2 МиБ).
const maxDiagBody = 2 << 20

// diagHeader — заголовок ответа провайдера.
type diagHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// diagReport — отчёт о разборе ответа: сколько узлов разобрано, сколько из них
// заглушек и какие прокси пропущены с причинами. Скрытых пропусков нет.
type diagReport struct {
	Parsed  int             `json:"parsed"`
	Stubs   int             `json:"stubs"`
	Skipped []nodes.Skipped `json:"skipped"`
}

// diagRecord — диагностика одного запроса подписки под одно ядро (D-07). Хранится
// в <data_dir>/config_layer/diag/<id источника>.<ядро>.json (0600) уже
// замаскированной (D-33, D-34); читает её 145-10 и отдаёт интерфейсу как есть.
type diagRecord struct {
	Kernel     string       `json:"kernel"`
	FetchedAt  time.Time    `json:"fetched_at"`
	HTTPStatus int          `json:"http_status"`
	Headers    []diagHeader `json:"headers"`
	Body       string       `json:"body"`
	Truncated  bool         `json:"truncated"`
	Report     diagReport   `json:"report"`
}

// sensitiveHeaders — заголовки, значения которых не записываются вовсе.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"proxy-authorization": true,
}

// diagPath — путь файла диагностики. id и ядро проверяются до построения пути:
// из них строится имя файла.
func (b blobStore) diagPath(id, kernel string) (string, bool) {
	if !idRE.MatchString(id) || id == MySourceID || (kernel != kernelXray && kernel != kernelMihomo) {
		return "", false
	}
	return filepath.Join(b.diagDir, id+"."+kernel+".json"), true
}

// writeDiag записывает диагностику запроса подписки под ядро: заголовки, тело до
// 2 МиБ и отчёт разбора. Всё маскируется до записи: секреты всех узлов ответа и
// адреса запроса заменяются на маску, адреса http(s) сокращаются до домена,
// значения Authorization, Cookie, Set-Cookie и Proxy-Authorization не хранятся.
// Каталог создаётся при первой записи (0700), файл — 0600. Сбой записи только
// в журнал: диагностика не должна ронять обновление.
func (s *Service) writeDiag(id, kernel string, fr fetchResult, pr nodes.ParseResult) {
	path, ok := s.blobs.diagPath(id, kernel)
	if !ok {
		return
	}
	secrets := append([]string(nil), fr.Secrets...)
	stubs := 0
	for _, n := range pr.Nodes {
		secrets = append(secrets, nodes.Secrets(n)...)
		if nodes.IsStub(n) {
			stubs++
		}
	}

	rec := diagRecord{
		Kernel:     kernel,
		FetchedAt:  s.d.Now(),
		HTTPStatus: fr.Status,
		Headers:    maskHeaders(fr.Header, secrets),
		Report:     diagReport{Parsed: len(pr.Nodes), Stubs: stubs, Skipped: maskSkipped(pr.Skipped, secrets)},
	}
	body := string(fr.Body)
	if pr.Format == "base64" {
		// Тело в base64 прячет учётные данные узлов от маскирования: хранится
		// расшифрованный текст, уже замаскированный.
		if dec, ok := decodeBase64Body(fr.Body); ok {
			body = dec
		}
	}
	// Усечение до маскирования ограничивает его стоимость на слабом роутере; срез
	// идёт по границе строки, чтобы секрет не оказался разрезанным. После маскирования
	// предел проверяется ещё раз: замены могут удлинить текст.
	body, cut := capLines(body, maxDiagBody)
	masked, cutMasked := capText(nodes.MaskText(body, secrets), maxDiagBody)
	rec.Body, rec.Truncated = masked, cut || cutMasked

	data, err := json.Marshal(rec)
	if err != nil {
		log.Printf("[sources] диагностика источника %s не сериализована", id)
		return
	}
	if err := os.MkdirAll(s.blobs.diagDir, 0o700); err != nil {
		log.Printf("[sources] каталог диагностики не создан")
		return
	}
	if err := utils.AtomicWriteFile(path, data, 0o600); err != nil {
		log.Printf("[sources] диагностика источника %s не записана", id)
	}
}

// maskHeaders — заголовки ответа в стабильном порядке; значения чувствительных
// заголовков заменены на «…», остальные замаскированы.
func maskHeaders(h http.Header, secrets []string) []diagHeader {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]diagHeader, 0, len(names))
	for _, name := range names {
		if sensitiveHeaders[strings.ToLower(name)] {
			out = append(out, diagHeader{Name: name, Value: "…"})
			continue
		}
		out = append(out, diagHeader{Name: name, Value: nodes.MaskText(strings.Join(h[name], ", "), secrets)})
	}
	return out
}

// maskSkipped возвращает копию пропусков с замаскированными текстами; срез не nil.
func maskSkipped(list []nodes.Skipped, secrets []string) []nodes.Skipped {
	out := make([]nodes.Skipped, 0, len(list))
	for _, sk := range list {
		sk.Name = nodes.MaskText(sk.Name, secrets)
		sk.Fragment = nodes.MaskText(sk.Fragment, secrets)
		if len(sk.Params) > 0 {
			params := make(map[string]string, len(sk.Params))
			for k, v := range sk.Params {
				params[k] = nodes.MaskText(v, secrets)
			}
			sk.Params = params
		}
		out = append(out, sk)
	}
	return out
}

// capText обрезает текст до limit байт по границе символа; второе значение —
// текст был усечён.
func capText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}

// capLines обрезает текст до limit байт по границе строки (если перевод строки
// есть в последних 4 КиБ), иначе по границе символа.
func capLines(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	head, _ := capText(text, limit)
	if i := strings.LastIndexByte(head, '\n'); i >= 0 && len(head)-i <= 4096 {
		return head[:i], true
	}
	return head, true
}

// decodeBase64Body расшифровывает тело подписки в base64 (алфавиты Std и URL, с
// выравниванием и без, возможны переносы строк).
func decodeBase64Body(body []byte) (string, bool) {
	compact := strings.Join(strings.Fields(string(body)), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(compact); err == nil {
			return string(raw), true
		}
	}
	return "", false
}

// DiagView — диагностика ответа подписки по ядру для интерфейса (D-07): файл,
// записанный при запросе, отдаётся как есть, он уже замаскирован.
type DiagView struct {
	Kernel     string       `json:"kernel"`
	Empty      bool         `json:"empty"`
	FetchedAt  *time.Time   `json:"fetched_at,omitempty"`
	HTTPStatus int          `json:"http_status,omitempty"`
	Headers    []diagHeader `json:"headers"`
	Body       string       `json:"body"`
	Truncated  bool         `json:"truncated"`
	Report     diagReport   `json:"report"`
}

// Diagnostics читает диагностику подписки по ядру. Ядро вне xray и mihomo —
// ErrInvalidInput; источник без секции в черновике и применённом состоянии —
// ErrSourceNotFound; нет записанной диагностики — признак Empty.
func (s *Service) Diagnostics(id, kernel string) (DiagView, error) {
	if kernel != kernelXray && kernel != kernelMihomo {
		return DiagView{}, ErrInvalidInput
	}
	if id == MySourceID {
		return DiagView{}, ErrInvalidInput
	}
	if !idRE.MatchString(id) {
		return DiagView{}, ErrSourceNotFound
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return DiagView{}, err
	}
	_, inDraft := st.Draft[sectionName(id)]
	_, inApplied := st.Applied[sectionName(id)]
	if !inDraft && !inApplied {
		return DiagView{}, ErrSourceNotFound
	}
	empty := DiagView{Kernel: kernel, Empty: true, Headers: []diagHeader{}, Report: diagReport{Skipped: []nodes.Skipped{}}}
	path, ok := s.blobs.diagPath(id, kernel)
	if !ok {
		return empty, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty, nil
		}
		return DiagView{}, err
	}
	var rec diagRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		log.Printf("[sources] диагностика источника %s повреждена", id)
		return empty, nil
	}
	v := DiagView{
		Kernel:     kernel,
		HTTPStatus: rec.HTTPStatus,
		Headers:    rec.Headers,
		Body:       rec.Body,
		Truncated:  rec.Truncated,
		Report:     rec.Report,
	}
	if !rec.FetchedAt.IsZero() {
		t := rec.FetchedAt
		v.FetchedAt = &t
	}
	if v.Headers == nil {
		v.Headers = []diagHeader{}
	}
	if v.Report.Skipped == nil {
		v.Report.Skipped = []nodes.Skipped{}
	}
	return v, nil
}
