package routing

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Имена секций черновика и ключ живых данных. Имена подходят под
// sectionNameRE слоя (^[a-z][a-z0-9_]{0,31}$).
const (
	// GroupsSection — секция групп.
	GroupsSection = "groups"
	// RulesSection — секция правил.
	RulesSection = "rules"
	// ProbeSection — секция параметров проверки узлов (одна на панель, D-12).
	ProbeSection = "probe"
	// LiveKeySelection — ключ State.Live с выбором участника ручных групп:
	// карта «ID группы → участник»; участник — "node:<ref>", "group:<id>",
	// "direct" или "reject" (пишет 146-14).
	LiveKeySelection = "selection"
)

// sectionVersion — версия формата всех секций модели.
const sectionVersion = 1

// ProbeSampling — число замеров, по которым оценивается узел (sampling
// burstObservatory Xray), общее для обоих ядер.
const ProbeSampling = 2

// Значения параметров проверки по умолчанию (D-12).
const (
	defaultProbeURL      = "https://www.gstatic.com/generate_204"
	defaultProbeTimeoutS = 5
	// probeIntervalFastS — интервал на обычных роутерах, probeIntervalSlowS — на
	// слабых (mips и mipsle).
	probeIntervalFastS = 30
	probeIntervalSlowS = 120
)

// GroupMode — режим группы.
type GroupMode string

// Режимы группы.
const (
	// ModeManual — участник выбирается вручную.
	ModeManual GroupMode = "manual"
	// ModeAuto — самый быстрый узел по проверке.
	ModeAuto GroupMode = "auto"
	// ModeFallback — самый быстрый из основных, резервный узел — при отказе всех.
	ModeFallback GroupMode = "fallback"
)

// Виды участников группы и целей правил.
const (
	KindGroup  = "group"
	KindDirect = "direct"
	KindReject = "reject"
)

// Типы условий правила (D-15).
const (
	CondDomain     = "domain"
	CondGeosite    = "geosite"
	CondIP         = "ip"
	CondGeoip      = "geoip"
	CondPort       = "port"
	CondNetwork    = "network"
	CondSrcIP      = "src_ip"
	CondBittorrent = "bittorrent"
)

// OriginPreset — пометка «из пресета» у правила (D-24).
const OriginPreset = "preset"

// groupTagPrefix — префикс тега Xray группы. Он не совпадает с префиксами тегов
// узлов источников (xcp-my-, xcp-<8 hex>-), поэтому селекторы узлов группу не
// захватывают (D-14).
const groupTagPrefix = "xcp-g-"

// GroupTag — тег группы в файлах Xray: строится из стабильного ID.
func GroupTag(id string) string { return groupTagPrefix + id }

// idRE — допустимый ID группы и правила: 8 hex. В алфавите [0-9a-f] нет слов
// стоп-списка XKeen, поэтому имена xcp-g-<id> стоп-список не задевают.
var idRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// refRE — ссылка на узел: <источник>/<8 hex> (nodes.Ref).
var refRE = regexp.MustCompile(`^(?:my|[0-9a-f]{8})/[0-9a-f]{8}$`)

// newID возвращает 8 hex из источника случайности.
func newID(r io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", errRandUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}

// GroupSources — какие источники входят в группу.
type GroupSources struct {
	// All — все источники (в том числе добавленные позже).
	All bool `json:"all"`
	// IDs — выбранные источники при All=false.
	IDs []string `json:"ids"`
}

// Member — участник ручной группы: другая группа, DIRECT или REJECT.
type Member struct {
	// Kind — group, direct или reject.
	Kind string `json:"kind"`
	// ID — ID группы при Kind=group.
	ID string `json:"id,omitempty"`
}

// Group — группа узлов, общая для обоих ядер (GRP-01).
type Group struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Mode GroupMode `json:"mode"`
	// Sources — выбранные источники или все (шаг 1 состава, D-11).
	Sources GroupSources `json:"sources"`
	// Include — «включить, если в имени есть» (шаг 2); пустой список не фильтрует.
	Include []string `json:"include"`
	// Exclude — «исключить, если есть» (шаг 3).
	Exclude []string `json:"exclude"`
	// Regex — Include и Exclude как регулярные выражения Go (режим «Эксперт»).
	Regex bool `json:"regex"`
	// Picks — отдельные узлы по ссылке (nodes.Ref); фильтрами не отсекаются.
	Picks []string `json:"picks"`
	// Members — участники ручной группы (D-08).
	Members []Member `json:"members"`
	// Reserve — резервный узел группы «резерв» (ссылка), отдельное поле (D-10).
	Reserve string `json:"reserve"`
}

// GroupsConfig — секция groups.
type GroupsConfig struct {
	V int `json:"v"`
	// DefaultsDone — группы по умолчанию уже создавались: удалённые заново не
	// появляются (D-37).
	DefaultsDone bool    `json:"defaults_done"`
	Items        []Group `json:"items"`
}

// Target — цель правила: группа, DIRECT или REJECT.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// Cond — условие правила: значения внутри условия объединяются через «ИЛИ».
type Cond struct {
	Type string `json:"type"`
	// Values — значения (домены, сети, порты, протоколы сети).
	Values []string `json:"values"`
	// Lists — списки geosite и geoip из dat-файлов XKeen.
	Lists []lists.ListRef `json:"lists"`
}

// RawRule — «своё правило ядра» (D-16): правило только одного ядра, для другого
// пропускается.
type RawRule struct {
	// Kernel — xray или mihomo.
	Kernel string `json:"kernel"`
	// Text — JSON-объект правила Xray или строка правила Mihomo (без цели).
	Text string `json:"text"`
}

// Rule — правило: условия объединяются через «И».
type Rule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Origin — "" (правило пользователя) или "preset" (D-24).
	Origin string   `json:"origin"`
	Conds  []Cond   `json:"conds"`
	Target Target   `json:"target"`
	Raw    *RawRule `json:"raw"`
}

// PresetState — выбор пресета, по которому созданы правила с пометкой «из
// пресета» (заполняет 146-10).
type PresetState struct {
	ID       string          `json:"id"`
	Country  string          `json:"country"`
	Services []string        `json:"services"`
	Flags    map[string]bool `json:"flags"`
}

// RulesConfig — секция rules.
type RulesConfig struct {
	V     int    `json:"v"`
	Items []Rule `json:"items"`
	// Final — цель правила «Остальное» (в Mihomo MATCH): всегда есть, не удаляется.
	Final  Target       `json:"final"`
	Preset *PresetState `json:"preset"`
}

// ProbeConfig — секция probe: параметры проверки узлов для «авто» и «резерва»,
// одни на панель и оба ядра (D-12).
type ProbeConfig struct {
	V         int    `json:"v"`
	URL       string `json:"url"`
	IntervalS int    `json:"interval_s"`
	TimeoutS  int    `json:"timeout_s"`
}

// isEmptyRaw — секции нет или она равна null.
func isEmptyRaw(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// normalizeGroup заменяет nil-срезы пустыми: в JSON нет null, клиент не
// проверяет каждое поле.
func normalizeGroup(g *Group) {
	if g.Sources.IDs == nil {
		g.Sources.IDs = []string{}
	}
	if g.Include == nil {
		g.Include = []string{}
	}
	if g.Exclude == nil {
		g.Exclude = []string{}
	}
	if g.Picks == nil {
		g.Picks = []string{}
	}
	if g.Members == nil {
		g.Members = []Member{}
	}
}

// normalizeRule — то же для правила и его условий.
func normalizeRule(r *Rule) {
	if r.Conds == nil {
		r.Conds = []Cond{}
	}
	for i := range r.Conds {
		if r.Conds[i].Values == nil {
			r.Conds[i].Values = []string{}
		}
		if r.Conds[i].Lists == nil {
			r.Conds[i].Lists = []lists.ListRef{}
		}
	}
}

// validMode сообщает, что режим из трёх допустимых.
func validMode(m GroupMode) bool {
	return m == ModeManual || m == ModeAuto || m == ModeFallback
}

// validTargetKind сообщает, что вид цели допустим.
func validTargetKind(k string) bool {
	return k == KindGroup || k == KindDirect || k == KindReject
}

// parseGroups разбирает секцию groups. Нет секции — пустой список групп. Мусор,
// неизвестная версия, плохой ID или режим — ErrInvalidInput: сообщить о порче
// обязана проверка (section_invalid), а не паника сервиса.
func parseGroups(raw json.RawMessage) (GroupsConfig, error) {
	if isEmptyRaw(raw) {
		return GroupsConfig{V: sectionVersion, Items: []Group{}}, nil
	}
	var cfg GroupsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return GroupsConfig{}, invalid(GroupsSection, reasonSectionInvalid)
	}
	if cfg.V != sectionVersion {
		return GroupsConfig{}, invalid(GroupsSection, reasonSectionInvalid)
	}
	if cfg.Items == nil {
		cfg.Items = []Group{}
	}
	seen := make(map[string]bool, len(cfg.Items))
	for i := range cfg.Items {
		g := &cfg.Items[i]
		if !idRE.MatchString(g.ID) || seen[g.ID] || !validMode(g.Mode) {
			return GroupsConfig{}, invalid(GroupsSection, reasonSectionInvalid)
		}
		seen[g.ID] = true
		normalizeGroup(g)
	}
	return cfg, nil
}

// encodeGroups сериализует секцию groups.
func encodeGroups(cfg GroupsConfig) (json.RawMessage, error) {
	cfg.V = sectionVersion
	if cfg.Items == nil {
		cfg.Items = []Group{}
	}
	for i := range cfg.Items {
		normalizeGroup(&cfg.Items[i])
	}
	return json.Marshal(cfg)
}

// parseRules разбирает секцию rules. Нет секции — пустой список и final DIRECT
// (D-04 «начать с пустого»).
func parseRules(raw json.RawMessage) (RulesConfig, error) {
	if isEmptyRaw(raw) {
		return RulesConfig{V: sectionVersion, Items: []Rule{}, Final: Target{Kind: KindDirect}}, nil
	}
	var cfg RulesConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return RulesConfig{}, invalid(RulesSection, reasonSectionInvalid)
	}
	if cfg.V != sectionVersion {
		return RulesConfig{}, invalid(RulesSection, reasonSectionInvalid)
	}
	if cfg.Final.Kind == "" {
		cfg.Final.Kind = KindDirect
	}
	if !validTargetKind(cfg.Final.Kind) {
		return RulesConfig{}, invalid(RulesSection, reasonSectionInvalid)
	}
	if cfg.Items == nil {
		cfg.Items = []Rule{}
	}
	seen := make(map[string]bool, len(cfg.Items))
	for i := range cfg.Items {
		r := &cfg.Items[i]
		if !idRE.MatchString(r.ID) || seen[r.ID] || !validTargetKind(r.Target.Kind) {
			return RulesConfig{}, invalid(RulesSection, reasonSectionInvalid)
		}
		seen[r.ID] = true
		normalizeRule(r)
	}
	return cfg, nil
}

// encodeRules сериализует секцию rules.
func encodeRules(cfg RulesConfig) (json.RawMessage, error) {
	cfg.V = sectionVersion
	if cfg.Items == nil {
		cfg.Items = []Rule{}
	}
	if cfg.Final.Kind == "" {
		cfg.Final.Kind = KindDirect
	}
	for i := range cfg.Items {
		normalizeRule(&cfg.Items[i])
	}
	return json.Marshal(cfg)
}

// defaultProbe — параметры проверки по умолчанию для архитектуры (D-12): на
// слабых роутерах (mips, mipsle) интервал реже.
func defaultProbe(goarch string) ProbeConfig {
	interval := probeIntervalFastS
	switch goarch {
	case "mips", "mipsle", "mips64", "mips64le":
		interval = probeIntervalSlowS
	}
	return ProbeConfig{V: sectionVersion, URL: defaultProbeURL, IntervalS: interval, TimeoutS: defaultProbeTimeoutS}
}

// parseProbe разбирает секцию probe поверх значений по умолчанию: нет секции или
// пропущенные поля — значения для архитектуры. Мусор и неизвестная версия —
// ErrInvalidInput; допустимость чисел проверяет validateProbe.
func parseProbe(raw json.RawMessage, goarch string) (ProbeConfig, error) {
	cfg := defaultProbe(goarch)
	if isEmptyRaw(raw) {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ProbeConfig{}, invalid(ProbeSection, reasonSectionInvalid)
	}
	if cfg.V != sectionVersion {
		return ProbeConfig{}, invalid(ProbeSection, reasonSectionInvalid)
	}
	return cfg, nil
}

// encodeProbe сериализует секцию probe.
func encodeProbe(cfg ProbeConfig) (json.RawMessage, error) {
	cfg.V = sectionVersion
	return json.Marshal(cfg)
}
