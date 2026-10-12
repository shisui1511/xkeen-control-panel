package sources

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Ключи живых данных слоя (State.Live), которые ведёт сервис источников.
const (
	// LiveKeySources — состояние подписок: метаданные провайдера и снимки по ядрам.
	LiveKeySources = "sources"
	// LiveKeyCandidates — кандидаты фонового обновления: id → ядро → снимок.
	// Живут только в оверлее одного запуска конвейера, в состоянии не сохраняются.
	LiveKeyCandidates = "candidates"
	// LiveKeyCandidatePrints — отпечатки конфигурации, под которой получены
	// кандидаты: id → sha256 от адреса подписки и HWID. Живут в том же оверлее.
	LiveKeyCandidatePrints = "candidate_prints"
)

// Коды состояния источника по ядру (Status.Code).
const (
	// StatusOK — последний запрос дал рабочий набор.
	StatusOK = "ok"
	// StatusRejected — провайдер отклонил устройство или ссылку (all_stubs, http_auth).
	StatusRejected = "rejected"
	// StatusProviderError — ответ получен, но подписки в нём нет (пустой, HTML, не разобран).
	StatusProviderError = "provider_error"
	// StatusNetwork — запрос не дошёл до провайдера (DNS, таймаут, TLS, SSRF, 5xx).
	StatusNetwork = "network"
	// StatusDeferred — новый снимок ждёт, пока решится дрейф файлов панели.
	StatusDeferred = "deferred"
	// StatusRefused — ядро не приняло новый состав при проверке.
	StatusRefused = "refused"
	// StatusNone — запроса под ядро ещё не было.
	StatusNone = "none"
)

// LiveSources — живые данные подписок: id источника → состояние.
type LiveSources map[string]*LiveSource

// LiveSource — живое состояние одной подписки.
type LiveSource struct {
	Meta    Meta                   `json:"meta"`
	Kernels map[string]*KernelLive `json:"kernels"`
}

// KernelLive — снимки и статус подписки по одному ядру (D-25): у каждого ядра
// свой последний рабочий набор и своя причина отказа.
type KernelLive struct {
	// Applied — набор, по которому построены применённые файлы ядра.
	Applied *Ref `json:"applied,omitempty"`
	// Pending — самый свежий рабочий набор, который ждёт «Применить».
	Pending   *Ref      `json:"pending,omitempty"`
	Status    Status    `json:"status"`
	Delta     Delta     `json:"delta"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Ref — ссылка на снимок набора узлов: файл <data_dir>/config_layer/snapshots/<Hash>.json.
type Ref struct {
	Hash      string    `json:"hash"`
	FetchedAt time.Time `json:"fetched_at"`
	// Format — формат ответа провайдера: clash, xray_json, singbox, base64, links, wg.
	Format string `json:"format"`
	// Total — всего узлов в ответе; Usable — рабочих (без заглушек); Stubs — заглушек;
	// Skipped — прокси, которые не стали узлами.
	Total   int `json:"total"`
	Usable  int `json:"usable"`
	Stubs   int `json:"stubs"`
	Skipped int `json:"skipped"`
}

// Status — состояние источника по ядру. Params несёт только безопасные значения
// (код HTTP, домен, дату): URL подписки и секреты сюда не попадают (D-33).
type Status struct {
	Code      string            `json:"code"`
	ErrorCode string            `json:"error_code,omitempty"`
	HTTP      int               `json:"http,omitempty"`
	Since     time.Time         `json:"since"`
	RetryAt   time.Time         `json:"retry_at,omitempty"`
	Attempts  int               `json:"attempts,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

// Delta — разница составов при последней фиксации: узлов добавлено и убрано.
type Delta struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// readLive разбирает Live["sources"]; отсутствие ключа — пустой набор.
func readLive(live configlayer.Sections) (LiveSources, error) {
	out := LiveSources{}
	raw, ok := live[LiveKeySources]
	if !ok || len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return LiveSources{}, fmt.Errorf("%w: живые данные источников: %v", ErrInvalidInput, err)
	}
	if out == nil {
		out = LiveSources{}
	}
	for id, src := range out {
		if src == nil {
			delete(out, id)
			continue
		}
		if src.Kernels == nil {
			src.Kernels = map[string]*KernelLive{}
		}
		for k, kl := range src.Kernels {
			if kl == nil {
				delete(src.Kernels, k)
			}
		}
	}
	return out, nil
}

// writeLive записывает Live["sources"]; пустой набор удаляет ключ.
func writeLive(live configlayer.Sections, ls LiveSources) error {
	if len(ls) == 0 {
		delete(live, LiveKeySources)
		return nil
	}
	raw, err := json.Marshal(ls)
	if err != nil {
		return err
	}
	live[LiveKeySources] = raw
	return nil
}

// Candidates — кандидаты обновления: id источника → ядро → снимок.
type Candidates map[string]map[string]Ref

// candidatePrint — отпечаток конфигурации подписки, от которой зависит ответ
// провайдера: адрес и HWID. Фильтр исключений в него не входит: он действует при
// сборке из секции и к ответу не относится.
func candidatePrint(cfg SubscriptionConfig) string {
	return hash(cfg.URL, cfg.HWID)
}

// readCandidates разбирает Live["candidates"] (оверлей запуска конвейера) и
// оставляет только кандидатов, чей отпечаток совпадает с секцией sections, из
// которой идёт сборка. Пока кандидат ждал конвейер, подписку могли применить с
// другим адресом или HWID: набор прежнего адреса к ней не относится и не должен
// ни попасть в файлы, ни стать применённым. Кандидат без отпечатка или без
// секции отбрасывается так же.
func readCandidates(live, sections configlayer.Sections) (Candidates, error) {
	out := Candidates{}
	raw, ok := live[LiveKeyCandidates]
	if !ok || len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Candidates{}, fmt.Errorf("%w: кандидаты источников: %v", ErrInvalidInput, err)
	}
	if out == nil {
		return Candidates{}, nil
	}
	prints := map[string]string{}
	if raw := live[LiveKeyCandidatePrints]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &prints); err != nil {
			return Candidates{}, fmt.Errorf("%w: отпечатки кандидатов: %v", ErrInvalidInput, err)
		}
	}
	for id := range out {
		if sec, ok := sections[sectionName(id)]; ok && prints[id] != "" {
			if cfg, err := parseSubscription(sec); err == nil && candidatePrint(cfg) == prints[id] {
				continue
			}
		}
		log.Printf("[sources] кандидаты источника %s отброшены: конфигурация подписки изменилась", id)
		delete(out, id)
	}
	return out, nil
}

// writeCandidates записывает Live["candidates"] и отпечатки конфигураций cfgs
// (id → конфигурация, под которой получены кандидаты); пустой набор удаляет оба
// ключа. Кандидат без конфигурации в cfgs при чтении отбрасывается.
func writeCandidates(live configlayer.Sections, c Candidates, cfgs map[string]SubscriptionConfig) error {
	if len(c) == 0 {
		delete(live, LiveKeyCandidates)
		delete(live, LiveKeyCandidatePrints)
		return nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	prints := make(map[string]string, len(cfgs))
	for id, cfg := range cfgs {
		prints[id] = candidatePrint(cfg)
	}
	praw, err := json.Marshal(prints)
	if err != nil {
		return err
	}
	live[LiveKeyCandidates] = raw
	live[LiveKeyCandidatePrints] = praw
	return nil
}

// kernelLive возвращает запись источника по ядру, создавая её при отсутствии.
func (src *LiveSource) kernelLive(kernel string) *KernelLive {
	if src.Kernels == nil {
		src.Kernels = map[string]*KernelLive{}
	}
	kl := src.Kernels[kernel]
	if kl == nil {
		kl = &KernelLive{Status: Status{Code: StatusNone}}
		src.Kernels[kernel] = kl
	}
	return kl
}

// setStatus записывает новый статус: Since сохраняется, пока код и причина те же,
// число попыток растёт на повторе неуспеха и сбрасывается при смене статуса.
func (kl *KernelLive) setStatus(st Status, now time.Time) {
	prev := kl.Status
	same := prev.Code == st.Code && prev.ErrorCode == st.ErrorCode && !prev.Since.IsZero()
	st.Since = now
	if same {
		st.Since = prev.Since
	}
	if st.Code != StatusOK && st.Code != StatusNone {
		st.Attempts = 1
		if same {
			st.Attempts = prev.Attempts + 1
		}
	}
	kl.Status = st
	kl.UpdatedAt = now
}
