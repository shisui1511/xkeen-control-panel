package configlayer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Секции ядер (SEC-01, SEC-02, MIHO-01): значение каждой секции хранится в
// черновике как «желание + значение + источник» (D-01), фактическое владение —
// в State.Cores (меняется только применением). Здесь — имена, файлы панели,
// формат секции черновика и записи состояния; чтение чужих файлов — в
// sections_scan.go.

// Имена секций черновика: подходят под sectionNameRE.
const (
	SectionXrayLog      = "xray_log"
	SectionXrayDNS      = "xray_dns"
	SectionXrayInbounds = "xray_inbounds"
	SectionXrayPolicy   = "xray_policy"
	SectionXrayOther    = "xray_other"

	SectionMihomoMain     = "mihomo_main"
	SectionMihomoDNS      = "mihomo_dns"
	SectionMihomoInbounds = "mihomo_inbounds"
	SectionMihomoSniffer  = "mihomo_sniffer"
	SectionMihomoOther    = "mihomo_other"
)

// Идентификаторы секций внутри ядра: имя секции черновика — CoreSectionName.
const (
	xraySectionLog      = "log"
	xraySectionDNS      = "dns"
	xraySectionInbounds = "inbounds"
	xraySectionPolicy   = "policy"
	xraySectionOther    = "other"
)

// XraySectionIDs и MihomoSectionIDs — идентификаторы секций ядер в порядке
// показа.
var (
	XraySectionIDs   = []string{xraySectionLog, xraySectionDNS, xraySectionInbounds, xraySectionPolicy, xraySectionOther}
	MihomoSectionIDs = []string{"main", "dns", "inbounds", "sniffer", "other"}
)

// CoreSectionName возвращает имя секции черновика по ядру и идентификатору
// секции; для неизвестного ядра — пустая строка.
func CoreSectionName(kernel, id string) string {
	switch kernel {
	case KernelXray:
		return "xray_" + id
	case KernelMihomo:
		return "mihomo_" + id
	}
	return ""
}

// Файлы панели в каталоге Xray. Все проходят xrayPanelName и не содержат слов
// стоп-списка XKeen. Файл «Входящих» сортируется сразу после чужого
// 03_inbounds.json: XKeen берёт порт прозрачного входящего из первого такого
// файла по порядку имён (143 Q7).
const (
	PanelXrayLogFile      = "99_xcp-log.json"
	PanelXrayDNSFile      = "99_xcp-dns.json"
	PanelXrayInboundsFile = "03_inbounds.xcp-inbounds.json"
	PanelXrayPolicyFile   = "99_xcp-policy.json"
	PanelXrayOtherFile    = "99_xcp-other.json"
	PanelXrayAPIFile      = "99_xcp-api.json"
)

// PanelXraySectionFile возвращает файл панели секции Xray; для неизвестной
// секции — пустая строка.
func PanelXraySectionFile(id string) string {
	switch id {
	case xraySectionLog:
		return PanelXrayLogFile
	case xraySectionDNS:
		return PanelXrayDNSFile
	case xraySectionInbounds:
		return PanelXrayInboundsFile
	case xraySectionPolicy:
		return PanelXrayPolicyFile
	case xraySectionOther:
		return PanelXrayOtherFile
	}
	return ""
}

// xrayForeignKeys — ключи верхнего уровня, которые ведут другие части панели
// (маршрутизация, наблюдение, исходящие, блок api) или запрещены в корне
// каталога: в «Прочие ключи» они не входят.
var xrayForeignKeys = map[string]bool{
	"routing":          true,
	"api":              true,
	"observatory":      true,
	"burstObservatory": true,
	"outbounds":        true,
	"transport":        true,
}

// XraySectionOfKey возвращает идентификатор секции Xray для ключа верхнего
// уровня: log, dns, inbounds, policy; ключи других частей панели (routing, api,
// observatory, burstObservatory, outbounds, transport) — пустая строка; любой
// другой ключ — other.
func XraySectionOfKey(key string) string {
	switch key {
	case xraySectionLog, xraySectionDNS, xraySectionInbounds, xraySectionPolicy:
		return key
	}
	if xrayForeignKeys[key] {
		return ""
	}
	return xraySectionOther
}

// SourceFile — чужой файл, из которого прочитано значение секции: имя в
// каталоге ядра и md5 содержимого на момент чтения (сверяется при «Применить»).
type SourceFile struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// coreSectionVersion — версия формата секции ядра в черновике.
const coreSectionVersion = 1

// CoreSection — содержимое секции ядра в черновике (D-01).
type CoreSection struct {
	V       int  `json:"v"`
	Managed bool `json:"managed"`
	// Value — значение секции целиком (объект или, для inbounds Xray, массив);
	// порядок ключей сохранён.
	Value json.RawMessage `json:"value,omitempty"`
	// From — файлы, прочитанные при взятии секции (имя, md5).
	From []SourceFile `json:"from,omitempty"`
	// Preset — значения взяты из пресета: чужого файла не было.
	Preset bool `json:"preset,omitempty"`
	// CommentsAck — владелец подтвердил потерю комментариев JSONC (D-02).
	CommentsAck bool `json:"comments_ack,omitempty"`
	// Keep — ключи, которые остаются в чужих файлах и не переносятся.
	Keep []string `json:"keep,omitempty"`
	// ForceProxyGroup — группа для входящих force-proxy* (ключ панели, в файл
	// ядра не попадает).
	ForceProxyGroup string `json:"force_proxy_group,omitempty"`
}

// ParseCoreSection разбирает секцию ядра. Пустой или null raw — секции нет:
// {V: 1, Managed: false}. Не объект, битый JSON или неизвестная версия — ошибка.
func ParseCoreSection(raw json.RawMessage) (CoreSection, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return CoreSection{V: coreSectionVersion}, nil
	}
	if trimmed[0] != '{' {
		return CoreSection{}, fmt.Errorf("секция ядра: ожидался объект")
	}
	var out CoreSection
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return CoreSection{}, fmt.Errorf("секция ядра: %w", err)
	}
	if out.V != coreSectionVersion {
		return CoreSection{}, fmt.Errorf("секция ядра: неизвестная версия %d", out.V)
	}
	return out, nil
}

// EncodeCoreSection кодирует секцию для черновика; нулевая версия заменяется
// текущей.
func EncodeCoreSection(cs CoreSection) (json.RawMessage, error) {
	if cs.V == 0 {
		cs.V = coreSectionVersion
	}
	b, err := json.Marshal(cs)
	if err != nil {
		return nil, fmt.Errorf("секция ядра: %w", err)
	}
	return b, nil
}

// coreSectionNames — десять известных имён секций черновика.
func coreSectionNames() []string {
	names := make([]string, 0, len(XraySectionIDs)+len(MihomoSectionIDs))
	for _, id := range XraySectionIDs {
		names = append(names, CoreSectionName(KernelXray, id))
	}
	for _, id := range MihomoSectionIDs {
		names = append(names, CoreSectionName(KernelMihomo, id))
	}
	return names
}

// DesiredCores читает секции ядер из секций черновика или применённого
// состояния: запись есть для каждого из десяти известных имён. Секции нет или
// она не разбирается — Managed=false: сообщить о порче секции отдельно обязаны
// генераторы и проверка (IssueCoreSectionInvalid через ParseCoreSection).
func DesiredCores(s Sections) map[string]CoreSection {
	out := make(map[string]CoreSection, len(XraySectionIDs)+len(MihomoSectionIDs))
	for _, name := range coreSectionNames() {
		cs, err := ParseCoreSection(s[name])
		if err != nil {
			cs = CoreSection{V: coreSectionVersion}
		}
		out[name] = cs
	}
	return out
}

// MihomoSectionsSaved — значения секций Mihomo сохранены в черновике: хотя бы одна
// секция Mihomo с Managed=false и непустым Value (D-09 из 147.1). Такие значения
// переживают отпускание и возвращаются в форму при повторном взятии.
func MihomoSectionsSaved(s Sections) bool {
	cores := DesiredCores(s)
	for _, id := range MihomoSectionIDs {
		cs := cores[CoreSectionName(KernelMihomo, id)]
		if !cs.Managed && len(bytes.TrimSpace(cs.Value)) > 0 {
			return true
		}
	}
	return false
}

// XraySectionState — фактическое владение одной секцией Xray: файл панели и
// перенесённые в резервный набор «чистые» файлы.
type XraySectionState struct {
	TakenAt time.Time `json:"taken_at"`
	// File — файл панели секции в каталоге Xray.
	File string `json:"file"`
	// Moved — «чистые» файлы секции, перенесённые в резервный набор панели.
	Moved []MovedFile `json:"moved"`
	// Left — файлы с ключом секции, оставленные на месте (смешанные и файлы
	// старого слоя).
	Left []string `json:"left"`
}

// CoresState — фактическое владение секциями ядер; меняется только применением
// (State.Cores). Секции Mihomo в состоянии не хранятся: профиль панели — один
// файл, он пересобирается целиком.
type CoresState struct {
	// Xray — секции Xray по идентификатору секции (log, dns, …).
	Xray map[string]*XraySectionState `json:"xray,omitempty"`
}

// isEmpty — ни одна секция не взята.
func (c CoresState) isEmpty() bool { return len(c.Xray) == 0 }

// clone возвращает глубокую копию.
func (c CoresState) clone() CoresState {
	out := CoresState{}
	if len(c.Xray) == 0 {
		return out
	}
	out.Xray = make(map[string]*XraySectionState, len(c.Xray))
	for id, s := range c.Xray {
		if s == nil {
			continue
		}
		cp := *s
		cp.Moved = append([]MovedFile{}, s.Moved...)
		cp.Left = append([]string{}, s.Left...)
		out.Xray[id] = &cp
	}
	return out
}

// backupDirs — наборы копий, на которые ссылается фактическое владение:
// ротация их не удаляет, иначе возврат не из чего было бы делать.
func (c CoresState) backupDirs() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range c.Xray {
		if s == nil {
			continue
		}
		for _, m := range s.Moved {
			if m.BackupDir == "" || seen[m.BackupDir] {
				continue
			}
			seen[m.BackupDir] = true
			out = append(out, m.BackupDir)
		}
	}
	return out
}

// PinnedValue — закреплённое значение ключа, которое панель дописывает поверх
// значения секции (например routing-mark у Mihomo).
type PinnedValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KeyIssue — проблема ключа секции для показа в форме.
type KeyIssue struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}

// Коды проблем секций ядер.
const (
	// IssueSectionTakeoverFailed — переносимый файл изменился между сканом и записью.
	IssueSectionTakeoverFailed = "section_takeover_failed"
	// IssueInboundsMixed — среди файлов с inbounds есть смешанный: «Входящие» не
	// берутся (иначе тег или порт задвоятся, D-25).
	IssueInboundsMixed = "inbounds_mixed"
	// IssueCommentsNotAck — в переносимых файлах есть комментарии JSONC, а
	// подтверждения потери нет (D-02).
	IssueCommentsNotAck = "comments_not_acknowledged"
	// IssueSectionShadowed — чужой файл с ключом секции сортируется после файла
	// панели и перекрыл бы его.
	IssueSectionShadowed = "section_shadowed"
	// IssueRawReserved — ключ в «сыром» блоке занят панелью.
	IssueRawReserved = "raw_reserved"
	// IssueRawPinned — ключ в «сыром» блоке закреплён и не редактируется.
	IssueRawPinned = "raw_pinned"
	// IssueRootKeyDuplicate — ключ корня профиля Mihomo уже задан в соседней
	// управляемой секции (main, inbounds, other): корень получил бы его дважды.
	IssueRootKeyDuplicate = "root_key_duplicate"
	// IssueControllerNoSecret — у внешнего контроллера Mihomo нет секрета.
	IssueControllerNoSecret = "controller_no_secret"
	// IssueInboundPort — порт входящего вне диапазона 1–65535.
	IssueInboundPort = "inbound_port_invalid"
	// IssueInboundDuplicate — два входящих с одним тегом или портом.
	IssueInboundDuplicate = "inbound_duplicate"
	// IssueCoreSectionInvalid — секция ядра в черновике не разбирается.
	IssueCoreSectionInvalid = "core_section_invalid"
)

// Дрейф и причины копий для секций ядер.
const (
	// DriftSectionForeign — после взятия секции появился чужой файл с её ключом.
	DriftSectionForeign = "section_foreign"
	// ReasonCoresTakeover — причина, по которой чужой файл попал в набор копий
	// при взятии секции.
	ReasonCoresTakeover = "cores_takeover"
)
