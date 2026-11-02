package cores

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Access-лог Xray через секцию log (PART-01, D-13…D-15, D-25): включение и
// выключение применяют только ключ log.access поверх применённого состояния, остальные
// правки черновика ждут «Применить». Секция за XKeen или пользователем берётся
// одним шагом без открытого черновика (перенос значения, копия файла), чужой файл
// панель не правит.

// logID — идентификатор секции log внутри Xray.
const logID = "log"

// accessKey — ключ access в значении секции log; accessOff — значение «выключено».
const (
	accessKey = "access"
	accessOff = "none"
)

// AccessLogView — действующее состояние access-лога Xray.
type AccessLogView struct {
	// Owner — владелец секции log: panel, xkeen, user или none.
	Owner string
	// Enabled — access не пуст и не none (без учёта регистра).
	Enabled bool
	// Path — действующее значение log.access как его сливает Xray.
	Path string
	// WillWrite — куда пойдёт журнал при включении: действующий путь, иначе
	// последний путь панели, иначе путь по умолчанию.
	WillWrite string
}

// accessOf читает log.access из значения секции log; значение не объект или без
// ключа — пустая строка.
func accessOf(value json.RawMessage) string {
	if isEmptyValue(value) {
		return ""
	}
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return ""
	}
	raw, ok := obj.Get(accessKey)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// accessEnabled сообщает, что значение access включает журнал.
func accessEnabled(access string) bool {
	return access != "" && !strings.EqualFold(access, accessOff)
}

// accessWillWrite выбирает путь, в который пойдёт журнал при включении.
func accessWillWrite(access, last string) string {
	switch {
	case accessEnabled(access):
		return access
	case last != "":
		return last
	}
	return services.DefaultXrayAccessLog
}

// withAccess возвращает значение секции log с заданным access: порядок ключей не
// меняется, новый ключ дописывается в конец. Пустое значение заменяется пресетом.
func withAccess(value json.RawMessage, access string) (json.RawMessage, error) {
	if isEmptyValue(value) {
		value = XrayPreset(logID)
	}
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, errCode(configlayer.IssueCoreSectionInvalid)
	}
	quoted, err := json.Marshal(access)
	if err != nil {
		return nil, err
	}
	obj.Set(accessKey, quoted)
	return obj.MarshalJSON()
}

// withAccessNone приводит значение секции log к виду, который пишется в файл ядра
// (D-16): без ключа access или с пустым значением Xray включает журнал в консоль,
// поэтому состояние «выключено» всегда пишется как access: none. Заданное значение
// и порядок существующих ключей не меняются, новый ключ дописывается в конец.
func withAccessNone(value json.RawMessage) (json.RawMessage, error) {
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, errCode(configlayer.IssueCoreSectionInvalid)
	}
	if raw, ok := obj.Get(accessKey); ok {
		var s string
		// null разбирается в пустую строку и тоже становится none; значение другого
		// типа остаётся как есть, его отклонит xray -test.
		if err := json.Unmarshal(raw, &s); err != nil || s != "" {
			return value, nil
		}
	}
	quoted, err := json.Marshal(accessOff)
	if err != nil {
		return nil, err
	}
	obj.Set(accessKey, quoted)
	return obj.MarshalJSON()
}

// lastAccessAfter возвращает путь, который секция помнит после действия: при
// включении — новый путь, при выключении — прежний, если журнал был включён.
func lastAccessAfter(prevLast, prevAccess, next string) string {
	if accessEnabled(next) {
		return next
	}
	if accessEnabled(prevAccess) {
		return prevAccess
	}
	return prevLast
}

// AccessLog возвращает действующее состояние access-лога. Секция у панели — по
// применённому значению; иначе по скану каталога Xray: значение сливается как у
// самого Xray (последний файл с log), владелец — из скана.
func (s *Service) AccessLog() (AccessLogView, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return AccessLogView{}, err
	}
	name := configlayer.SectionXrayLog
	applied, _ := configlayer.ParseCoreSection(st.Applied[name])
	draft, _ := configlayer.ParseCoreSection(st.Draft[name])
	last := applied.LastAccess
	if last == "" {
		last = draft.LastAccess
	}
	if s.d.Layer.Snapshot().Cores.Xray[logID].Taken && applied.Managed {
		access := accessOf(applied.Value)
		return AccessLogView{
			Owner:     ownerPanel,
			Enabled:   accessEnabled(access),
			Path:      access,
			WillWrite: accessWillWrite(access, last),
		}, nil
	}
	scans, _, err := s.d.Layer.ScanXraySections()
	if err != nil {
		return AccessLogView{}, err
	}
	sc := scans[logID]
	access := accessOf(sc.Effective)
	owner := sc.Owner
	if owner == "" {
		owner = configlayer.OwnerNone
	}
	return AccessLogView{
		Owner:     owner,
		Enabled:   accessEnabled(access),
		Path:      access,
		WillWrite: accessWillWrite(access, last),
	}, nil
}

// SetAccessLog включает или выключает access-лог (D-13, D-14).
//
// Секция log у панели: применяется только log.access поверх применённого
// состояния (запуск секции слоя): xray -test, запись, перезапуск запущенного
// ядра; значение дописано и в черновик, остальные его правки остаются
// неприменёнными. Секция за XKeen или пользователем: без черновика она берётся и
// применяется одним шагом (правка и применение слоя) — значение переносится, файл уходит в
// копии, комментарии пропадают только с ack_comments; при открытом черновике —
// configlayer.ErrDraftOpen. Xray должен быть установлен. Остановленное ядро не
// запускается: исход перезапуска deferred, решение за интерфейсом (D-17).
//
// dirOK проверяет путь журнала: вернув очищенный путь и true, он разрешает
// создать каталог журнала (MkdirAll 0755) до запуска конвейера; false — каталог
// не трогается, включение идёт. nil — каталог не создаётся. Сбой создания
// каталога — ошибка, конвейер не запускается.
func (s *Service) SetAccessLog(ctx context.Context, enabled, ackComments bool, dirOK func(path string) (string, bool)) (configlayer.ApplyView, error) {
	if err := s.needXray(configlayer.KernelXray); err != nil {
		return configlayer.ApplyView{}, err
	}
	view, err := s.AccessLog()
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	applied, _ := configlayer.ParseCoreSection(st.Applied[configlayer.SectionXrayLog])
	panelOwned := view.Owner == ownerPanel && applied.Managed
	if !panelOwned && s.d.Layer.Snapshot().DraftChanges != 0 {
		// Чужую секцию берёт и применяет слой целиком: открытый черновик он тоже
		// применил бы. Отказ до создания каталога.
		return configlayer.ApplyView{}, configlayer.ErrDraftOpen
	}

	path := accessOff
	if enabled {
		path = view.WillWrite
		if err := ensureLogDir(path, dirOK); err != nil {
			return configlayer.ApplyView{}, err
		}
	}
	if panelOwned {
		return s.setPanelAccess(ctx, applied, path)
	}
	return s.takeAccess(ctx, path, ackComments)
}

// ensureLogDir создаёт каталог журнала, если путь прошёл проверку dirOK.
func ensureLogDir(path string, dirOK func(string) (string, bool)) error {
	if dirOK == nil {
		return nil
	}
	clean, ok := dirOK(path)
	if !ok {
		return nil
	}
	return os.MkdirAll(filepath.Dir(clean), 0o755)
}

// setPanelAccess меняет log.access в секции панели поверх применённого состояния.
func (s *Service) setPanelAccess(ctx context.Context, applied configlayer.CoreSection, path string) (configlayer.ApplyView, error) {
	prevAccess := accessOf(applied.Value)
	next := applied
	value, err := withAccess(applied.Value, path)
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	next.Value = value
	next.LastAccess = lastAccessAfter(applied.LastAccess, prevAccess, path)
	raw, err := encodeSection(next)
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	return s.d.Layer.RunSectionLive(ctx, configlayer.SectionLiveRun{
		Trigger:  configlayer.TriggerAccessLog,
		Kernel:   configlayer.KernelXray,
		Sections: configlayer.Sections{configlayer.SectionXrayLog: raw},
		MergeDraft: func(_ string, draft json.RawMessage) (json.RawMessage, error) {
			return mergeAccessDraft(draft, path)
		},
	})
}

// mergeAccessDraft сливает access и последний путь в секцию log черновика. Секция,
// которой панель не управляет (нет, отпущена или не разбирается), не меняется;
// остальные ключи значения и поля секции остаются как были.
func mergeAccessDraft(draft json.RawMessage, path string) (json.RawMessage, error) {
	cs, err := configlayer.ParseCoreSection(draft)
	if err != nil || !cs.Managed {
		return draft, nil
	}
	prevAccess := accessOf(cs.Value)
	value, err := withAccess(cs.Value, path)
	if err != nil {
		return draft, nil
	}
	cs.Value = value
	cs.LastAccess = lastAccessAfter(cs.LastAccess, prevAccess, path)
	return encodeSection(cs)
}

// takeAccess берёт секцию log за XKeen или пользователем и записывает в неё
// access одним применением. Секция строится как у Take для одной
// секции log: действующее значение, файлы-источники с md5, согласие на потерю
// комментариев, блокировки; файла нет — пресет.
func (s *Service) takeAccess(ctx context.Context, path string, ackComments bool) (configlayer.ApplyView, error) {
	return s.d.Layer.ApplyEdit(ctx, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		scans, files, err := s.d.Layer.ScanXraySections()
		if err != nil {
			return nil, err
		}
		sc := scans[logID]
		if code, file := xrayBlock(logID, sc, files, false); code != "" {
			return nil, errCode(code, "file", file)
		}
		var commentFiles []string
		for _, name := range sc.Comments {
			if slices.Contains(sc.Pure, name) {
				commentFiles = append(commentFiles, name)
			}
		}
		if len(commentFiles) > 0 && !ackComments {
			return nil, errCode(configlayer.IssueCommentsNotAck, "file", commentFiles[0])
		}
		prev := configlayer.DesiredCores(st.Draft)[configlayer.SectionXrayLog]
		next := configlayer.CoreSection{
			Managed:         true,
			Value:           sc.Effective,
			From:            sc.Sources,
			CommentsAck:     ackComments && len(commentFiles) > 0,
			ForceProxyGroup: prev.ForceProxyGroup,
			LastAccess:      prev.LastAccess,
		}
		if len(sc.Files) == 0 || isEmptyValue(next.Value) {
			next.Value = XrayPreset(logID)
			next.Preset = true
		}
		prevAccess := accessOf(next.Value)
		value, err := withAccess(next.Value, path)
		if err != nil {
			return nil, err
		}
		next.Value = value
		next.LastAccess = lastAccessAfter(next.LastAccess, prevAccess, path)
		raw, err := encodeSection(next)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{configlayer.SectionXrayLog: raw}, nil
	})
}
