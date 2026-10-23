package sources

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Идентификаторы источников и секций.
const (
	// MySourceID — идентификатор источника «Мои узлы».
	MySourceID = "my"
	// MySection — имя секции черновика источника «Мои узлы».
	MySection = "src_my"
	// SectionPrefix — префикс имён секций источников.
	SectionPrefix = "src_"
)

// Виды источников (поле kind секции).
const (
	KindMy           = "my"
	KindSubscription = "subscription"
)

// Режимы интервала обновления подписки.
const (
	IntervalProvider = "provider"
	IntervalHours    = "hours"
	IntervalOff      = "off"
)

// idRE — допустимый идентификатор источника: «my» или 8 hex. В алфавите [0-9a-f]
// нет слов стоп-списка XKeen, поэтому имена файлов xcp-<id> его не нарушают.
var idRE = regexp.MustCompile(`^(?:my|[0-9a-f]{8})$`)

// MyEntry — запись «Моих узлов»: исходный текст узла целиком (share-ссылка,
// wg.conf, JSON или YAML). ID назначается при добавлении и не меняется при
// правке текста: на него опирается тег Xray xcp-my-<id> и ссылки групп.
type MyEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Raw    string `json:"raw"`
}

// MySourceConfig — секция src_my.
type MySourceConfig struct {
	Kind    string    `json:"kind"`
	Enabled bool      `json:"enabled"`
	Entries []MyEntry `json:"entries"`
}

// Interval — интервал фонового обновления подписки.
type Interval struct {
	Mode  string `json:"mode"`
	Hours int    `json:"hours,omitempty"`
}

// SubscriptionConfig — секция src_<id> подписки (наполняется 145-08).
type SubscriptionConfig struct {
	Kind     string    `json:"kind"`
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Enabled  bool      `json:"enabled"`
	Exclude  []string  `json:"exclude"`
	Interval Interval  `json:"interval"`
	HWID     string    `json:"hwid,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// sectionName — имя секции источника.
func sectionName(id string) string { return SectionPrefix + id }

// sourceID выделяет идентификатор источника из имени секции. Имя без префикса
// src_ и идентификатор вне алфавита файлов панели не принимаются: из них не
// строится путь файла.
func sourceID(section string) (string, bool) {
	id, ok := strings.CutPrefix(section, SectionPrefix)
	if !ok || !idRE.MatchString(id) {
		return "", false
	}
	return id, true
}

// SourcePresent сообщает, считается ли секция черновика источником для групп по
// умолчанию: секция src_* есть, а «Мои узлы» — с хотя бы одной записью. «Мои
// узлы», из которых удалены все узлы, источником не считаются; испорченная секция
// считается (её сообщает проверка).
func SourcePresent(section string, raw json.RawMessage) bool {
	if !strings.HasPrefix(section, SectionPrefix) {
		return false
	}
	if section != MySection {
		return true
	}
	if len(raw) == 0 {
		return false
	}
	cfg, err := parseMy(raw)
	return err != nil || len(cfg.Entries) > 0
}

// newID возвращает 8 hex из источника случайности.
func newID(r io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("sources: случайность недоступна: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// kindOf читает поле kind секции.
func kindOf(raw json.RawMessage) string {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return ""
	}
	return head.Kind
}

// parseMy разбирает секцию src_my; пустая или отсутствующая секция — новый
// включённый источник без записей.
func parseMy(raw json.RawMessage) (MySourceConfig, error) {
	if len(raw) == 0 {
		return MySourceConfig{Kind: KindMy, Enabled: true, Entries: []MyEntry{}}, nil
	}
	var cfg MySourceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return MySourceConfig{}, fmt.Errorf("%w: секция %s: %v", ErrInvalidInput, MySection, err)
	}
	cfg.Kind = KindMy
	if cfg.Entries == nil {
		cfg.Entries = []MyEntry{}
	}
	return cfg, nil
}

// encodeMy сериализует секцию src_my.
func encodeMy(cfg MySourceConfig) (json.RawMessage, error) {
	cfg.Kind = KindMy
	if cfg.Entries == nil {
		cfg.Entries = []MyEntry{}
	}
	return json.Marshal(cfg)
}
