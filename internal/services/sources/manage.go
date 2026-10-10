package sources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Пределы значений настроек источника (U-04, D-04, D-05, D-06).
const (
	maxNameRunes     = 128
	maxExcludeCount  = 100
	maxExcludeRunes  = 200
	maxHWIDLen       = 128
	maxSourceURLLen  = 2048
	maxExcludeIntake = 1000
)

// allowedIntervalHours — допустимые значения интервала обновления в часах.
var allowedIntervalHours = map[int]bool{1: true, 3: true, 6: true, 12: true, 24: true}

// Patch — правка настроек источника. Заданные поля заменяют прежние значения,
// остальные не меняются. У источника «Мои узлы» допустимо только Enabled. Ни
// флажков ядер, ни настроек sockopt, ни фильтров кроме исключения по имени в
// правке нет (U-03, D-04, D-09).
type Patch struct {
	Name     *string   `json:"name,omitempty"`
	Enabled  *bool     `json:"enabled,omitempty"`
	Exclude  *[]string `json:"exclude,omitempty"`
	Interval *Interval `json:"interval,omitempty"`
	HWID     *string   `json:"hwid,omitempty"`
	URL      *string   `json:"url,omitempty"`
}

func (p Patch) empty() bool {
	return p.Name == nil && p.Enabled == nil && p.Exclude == nil && p.Interval == nil && p.HWID == nil && p.URL == nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// cleanName проверяет имя источника: до 128 рун, без управляющих символов. Пустое
// имя допустимо — представление возьмёт название из ответа провайдера или домен.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", invalid("имя длиннее %d символов", maxNameRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return "", invalid("имя содержит недопустимые символы")
		}
	}
	return name, nil
}

// cleanExclude обрезает шаблоны исключения, отбрасывает пустые и проверяет пределы.
func cleanExclude(list []string) ([]string, error) {
	if len(list) > maxExcludeIntake {
		return nil, invalid("слишком много шаблонов исключения")
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if utf8.RuneCountInString(p) > maxExcludeRunes {
			return nil, invalid("шаблон исключения длиннее %d символов", maxExcludeRunes)
		}
		for _, r := range p {
			if unicode.IsControl(r) || r == utf8.RuneError {
				return nil, invalid("шаблон исключения содержит недопустимые символы")
			}
		}
		out = append(out, p)
	}
	if len(out) > maxExcludeCount {
		return nil, invalid("больше %d шаблонов исключения", maxExcludeCount)
	}
	return out, nil
}

// cleanInterval проверяет интервал: provider, off или hours из допустимого набора.
func cleanInterval(iv Interval) (Interval, error) {
	switch iv.Mode {
	case IntervalProvider, IntervalOff:
		return Interval{Mode: iv.Mode}, nil
	case IntervalHours:
		if !allowedIntervalHours[iv.Hours] {
			return Interval{}, invalid("интервал в часах: допустимо 1, 3, 6, 12, 24")
		}
		return Interval{Mode: IntervalHours, Hours: iv.Hours}, nil
	}
	return Interval{}, invalid("режим интервала")
}

// cleanHWID проверяет свой HWID источника: печатные ASCII без пробелов, до 128
// знаков; пустое значение снимает свой HWID (действует общий).
func cleanHWID(h string) (string, error) {
	h = strings.TrimSpace(h)
	if len(h) > maxHWIDLen {
		return "", invalid("HWID длиннее %d символов", maxHWIDLen)
	}
	for i := 0; i < len(h); i++ {
		if h[i] <= 0x20 || h[i] >= 0x7f {
			return "", invalid("HWID содержит недопустимые символы")
		}
	}
	return h, nil
}

// cleanURL проверяет ссылку подписки: http или https с хостом.
func cleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxSourceURLLen {
		return "", invalid("ссылка подписки")
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", invalid("ссылка подписки содержит недопустимые символы")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", invalid("ссылка подписки: нужен адрес http или https")
	}
	return raw, nil
}

// Update правит настройки источника в черновике с проверкой ревизии: устаревшая
// ревизия — configlayer.ErrDraftConflict без записи. Значения проверяются по
// пределам; неизвестный источник — ErrSourceNotFound. Смена URL сохраняет
// идентификатор источника и снимает ожидающий снимок: набор из прежнего адреса
// к новому не относится, новый ответ придёт при следующем обновлении.
func (s *Service) Update(rev int64, id string, p Patch) (configlayer.DraftEvent, error) {
	if p.empty() {
		return configlayer.DraftEvent{}, invalid("нечего менять")
	}
	if !idRE.MatchString(id) {
		return configlayer.DraftEvent{}, ErrSourceNotFound
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	raw, ok := st.Draft[sectionName(id)]
	if !ok {
		return configlayer.DraftEvent{}, ErrSourceNotFound
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}

	var section json.RawMessage
	urlChanged := false
	if id == MySourceID {
		cfg, err := parseMy(raw)
		if err != nil {
			return configlayer.DraftEvent{}, err
		}
		if p.Enabled == nil || p.Name != nil || p.Exclude != nil || p.Interval != nil || p.HWID != nil || p.URL != nil {
			return configlayer.DraftEvent{}, invalid("у источника «Мои узлы» меняется только включение")
		}
		cfg.Enabled = *p.Enabled
		if section, err = encodeMy(cfg); err != nil {
			return configlayer.DraftEvent{}, err
		}
	} else {
		cfg, err := parseSubscription(raw)
		if err != nil {
			return configlayer.DraftEvent{}, err
		}
		cfg.ID = id
		if p.Name != nil {
			if cfg.Name, err = cleanName(*p.Name); err != nil {
				return configlayer.DraftEvent{}, err
			}
		}
		if p.Enabled != nil {
			cfg.Enabled = *p.Enabled
		}
		if p.Exclude != nil {
			if cfg.Exclude, err = cleanExclude(*p.Exclude); err != nil {
				return configlayer.DraftEvent{}, err
			}
		}
		if p.Interval != nil {
			if cfg.Interval, err = cleanInterval(*p.Interval); err != nil {
				return configlayer.DraftEvent{}, err
			}
		}
		if p.HWID != nil {
			if cfg.HWID, err = cleanHWID(*p.HWID); err != nil {
				return configlayer.DraftEvent{}, err
			}
		}
		if p.URL != nil {
			u, err := cleanURL(*p.URL)
			if err != nil {
				return configlayer.DraftEvent{}, err
			}
			urlChanged = u != cfg.URL
			cfg.URL = u
		}
		if cfg.Exclude == nil {
			cfg.Exclude = []string{}
		}
		if section, err = json.Marshal(cfg); err != nil {
			return configlayer.DraftEvent{}, err
		}
	}

	var liveFn func(configlayer.Sections) error
	if urlChanged {
		liveFn = func(l configlayer.Sections) error {
			cur, err := readLive(l)
			if err != nil {
				return err
			}
			if src := cur[id]; src != nil {
				for _, kl := range src.Kernels {
					kl.Pending = nil
				}
			}
			return writeLive(l, cur)
		}
	}
	return s.d.Layer.EditDraftBatch(rev, map[string]json.RawMessage{sectionName(id): section}, liveFn)
}

// Delete убирает подписку из черновика. Живые данные и файлы источника удаляются
// при «Применить»; подписка, которой нет в применённом состоянии, теряет живые
// данные сразу. Источник «Мои узлы» не удаляется: узлы убираются по одному
// (DeleteNode). Проверка ссылок на узлы удаляемой подписки — фаза 146 (SRC-07).
func (s *Service) Delete(rev int64, id string) (configlayer.DraftEvent, error) {
	if id == MySourceID {
		return configlayer.DraftEvent{}, invalid("источник «Мои узлы» не удаляется")
	}
	if !idRE.MatchString(id) {
		return configlayer.DraftEvent{}, ErrSourceNotFound
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if _, ok := st.Draft[sectionName(id)]; !ok {
		return configlayer.DraftEvent{}, ErrSourceNotFound
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	_, applied := st.Applied[sectionName(id)]
	var liveFn func(configlayer.Sections) error
	if !applied {
		liveFn = func(l configlayer.Sections) error {
			cur, err := readLive(l)
			if err != nil {
				return err
			}
			delete(cur, id)
			return writeLive(l, cur)
		}
	}
	return s.d.Layer.EditDraftBatch(rev, map[string]json.RawMessage{sectionName(id): nil}, liveFn)
}

// Reveal возвращает полный URL подписки по явному действию пользователя (D-33):
// из черновика, а если источника там нет — из применённого состояния. В журнал
// адрес не пишется.
func (s *Service) Reveal(id string) (string, error) {
	if id == MySourceID {
		return "", invalid("у источника «Мои узлы» нет ссылки")
	}
	if !idRE.MatchString(id) {
		return "", ErrSourceNotFound
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return "", err
	}
	raw, ok := st.Draft[sectionName(id)]
	if !ok {
		raw, ok = st.Applied[sectionName(id)]
	}
	if !ok {
		return "", ErrSourceNotFound
	}
	cfg, err := parseSubscription(raw)
	if err != nil {
		return "", err
	}
	return cfg.URL, nil
}
