package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/proxyping"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Выбор участника ручной группы без черновика (D-36…D-39). Выбор хранится в
// State.Live[selection] по ID группы и один на оба ядра; Xray читает его при
// сборке файлов (xcp-groups), Mihomo получает его через Clash API.

// LiveKeyRepick — ключ State.Live со списком ID групп, в которых выбранный узел
// пропал: после применения панель заменяет его первым живым по пингу (D-36).
const LiveKeyRepick = "selection_repick"

// LiveKeyUnconfirmed хранит ID группы → ядро с неизвестным выбором (D-16).
// Аддитивный ключ Live переживает остановку панели, миграции не требует.
const LiveKeyUnconfirmed = "selection_unconfirmed"

// Исходы выбора по ядру (SelectResult.Kernels).
const (
	selApplied        = "applied"
	selRestarted      = "restarted"
	selPendingStopped = "pending_stopped"
	selNotInstalled   = "not_installed"
	selFailed         = "failed"
)

// Параметры выбора и сверки.
const (
	// selectBusyWait — сколько запуск выбора Xray ждёт замок применения, занятый
	// не выбором («Применить», фоновый запуск, операция с ядром), прежде чем
	// ответить apply_busy. Другие выборы этот замок не держат: их упорядочивает
	// xrayRunGate.
	selectBusyWait = 2 * time.Second
	// reconcileCallTimeout — предел одного обращения к Clash API при сверке.
	reconcileCallTimeout = 10 * time.Second
	// repickMaxProbe — сколько узлов группы пингуется за один проход.
	repickMaxProbe = 48
	// repickBusyRetries и repickBusyDelay — повторы выбора при идущем применении.
	repickBusyRetries = 5
	repickBusyDelay   = 30 * time.Second
)

// Префиксы уведомлений выбора в слое (Layer.SetNotices).
const (
	noticeReconcilePrefix = "routing_reconcile:"
	noticeRepickPrefix    = "routing_repick:"
	// noticeSelectPrefix — отказ фонового прохода файла Xray после раннего ответа
	// выбора: routing_select:<gid>:<n>, n — новый номер на каждый отказ (D-11).
	noticeSelectPrefix = "routing_select:"
	noticeWarning      = "warning"
)

// Классы причины отказа в уведомлении routing_select (параметр reason).
const (
	selReasonRejected = "rejected"
	selReasonBusy     = "busy"
	selReasonStopped  = "stopped"
	selReasonFailed   = "failed"
)

// Способ проверки в уведомлении routing_repick.
const (
	repickMethodDelay  = "delay"
	repickMethodTunnel = "tunnel"
	repickMethodTLS    = "tls"
)

// Ошибки выбора участника (коды group_not_applied, member_invalid, apply_busy).
var (
	// ErrGroupNotApplied — группы нет в применённом состоянии, она не ручная или
	// маршрутизацией не управляет панель.
	ErrGroupNotApplied = errors.New("routing: группа не применена")
	// ErrMemberInvalid — участника нет среди вариантов группы.
	ErrMemberInvalid = errors.New("routing: участник вне состава группы")
	// ErrApplyBusy — идёт применение: выбор Xray дождался бы его конца.
	ErrApplyBusy            = configlayer.ErrApplyBusy
	ErrSelectBusy           = errors.New("routing: идёт переключение связанной группы")
	ErrSelectionUnconfirmed = errors.New("routing: активный узел группы не подтверждён")
	ErrConfirmFailed        = errors.New("routing: сверка выбора не удалась")
	// errReconcileRetry — Clash API не ответил: сверку надо повторить позже.
	errReconcileRetry = errors.New("routing: Clash API Mihomo не ответил")
)

// SelectResult — итог выбора участника по ядрам.
type SelectResult struct {
	// Member — выбранный участник ("node:<ref>", "group:<id>", "direct", "reject").
	Member string `json:"member"`
	// Kernels — исход по ядрам: applied, restarted, pending_stopped,
	// not_installed или failed. Mihomo установлен, но не запущен —
	// pending_stopped (выбор поставит сверка при запуске процесса).
	Kernels       map[string]string `json:"kernels"`
	RestartReason string            `json:"restart_reason,omitempty"`
	Reverted      bool              `json:"reverted,omitempty"`
	Unconfirmed   []string          `json:"unconfirmed,omitempty"`
}

// --- применённое состояние ---

// appliedInput — вход генераторов для применённого состояния.
func appliedInput(st configlayer.StateView) configlayer.Input {
	return configlayer.Input{Sections: st.Applied, Live: st.Live, Mode: configlayer.SourceApplied}
}

// unionCatalog — узлы обоих ядер без повторов по ссылке: Xray, затем остальные
// узлы Mihomo.
func unionCatalog(cat sources.Catalog) []sources.CatalogNode {
	seen := make(map[string]bool, len(cat.Xray)+len(cat.Mihomo))
	out := make([]sources.CatalogNode, 0, len(cat.Xray)+len(cat.Mihomo))
	for _, list := range [][]sources.CatalogNode{cat.Xray, cat.Mihomo} {
		for _, n := range list {
			if !seen[n.Ref] {
				seen[n.Ref] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// appliedModel — группы и каталог узлов применённого состояния.
type appliedModel struct {
	groups    GroupsConfig
	cat       sources.Catalog
	union     []sources.CatalogNode
	byRef     map[string]sources.CatalogNode
	installed configlayer.InstalledKernels
	// xrayVer, mihomoVer — версии ядер, прочитанные до расчёта каталога: от них
	// зависят версионные ворота узлов каталога и групп Mihomo (catalogInputSame).
	xrayVer, mihomoVer string
}

// appliedModelOf читает группы и каталог узлов применённого состояния.
func (s *Service) appliedModelOf(st configlayer.StateView) (appliedModel, error) {
	cfg, err := parseGroups(st.Applied[GroupsSection])
	if err != nil {
		return appliedModel{}, err
	}
	m := appliedModel{groups: cfg, installed: s.d.Installed(), byRef: map[string]sources.CatalogNode{},
		xrayVer: s.d.KernelVersion(configlayer.KernelXray), mihomoVer: s.d.KernelVersion(configlayer.KernelMihomo)}
	if m.installed.Xray || m.installed.Mihomo {
		cat, err := s.d.Sources.Catalog(appliedInput(st), m.installed)
		if err != nil {
			return appliedModel{}, err
		}
		m.cat = cat
		m.union = unionCatalog(cat)
		for _, n := range m.union {
			m.byRef[n.Ref] = n
		}
	}
	return m, nil
}

// group возвращает группу по ID.
func (m appliedModel) group(id string) (Group, bool) {
	for _, g := range m.groups.Items {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// groupAlive — вложенная группа не пуста по правилу D-36: то же, что у проверки
// до «Применить» и генераторов (newGroupEmptiness). Считается по объединённому
// каталогу: выбор один на оба ядра (D-39), а группа непуста в объединении тогда и
// только тогда, когда непуста хотя бы в одном ядре (узлы объединения — объединение
// узлов ядер, вложенная группа непуста в объединении, если непуста в каком-то
// ядре). Поэтому «Сейчас», CommitLive и повторный выбор не ставят вложенную группу,
// которую оба ядра блокируют. Помощник строится на каждый вызов: групп десятки,
// кэш в appliedModel потребовал бы правки newModel.
func (m appliedModel) groupAlive(id string) bool {
	if _, ok := m.group(id); !ok {
		return false
	}
	return !newGroupEmptiness(m.groups.Items, m.union).empty(id)
}

// avail сообщает, что участник существует и пригоден для выбора.
func (m appliedModel) avail(member string) bool {
	switch {
	case member == memberDirect || member == memberReject:
		return true
	case strings.HasPrefix(member, memberNode):
		_, ok := m.byRef[strings.TrimPrefix(member, memberNode)]
		return ok
	case strings.HasPrefix(member, memberGroup):
		return m.groupAlive(strings.TrimPrefix(member, memberGroup))
	}
	return false
}

// candidates — участники ручной группы по порядку: участники-группы, DIRECT и
// REJECT, затем узлы состава. Порядок тот же, что у effectiveSelection.
func (m appliedModel) candidates(g Group) []string {
	r := Resolve(g, m.union, m.groups.Items)
	out := make([]string, 0, len(r.Members)+len(r.Nodes))
	for _, mem := range r.Members {
		if s := memberString(mem); s != "" {
			out = append(out, s)
		}
	}
	for _, n := range r.Nodes {
		out = append(out, memberNode+n.Ref)
	}
	return out
}

// --- запись выбора в Live ---

func readUnconfirmed(live configlayer.Sections) map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(live[LiveKeyUnconfirmed], &m); err != nil || m == nil {
		return map[string]string{}
	}
	return m
}

func writeUnconfirmed(live configlayer.Sections, m map[string]string) error {
	if len(m) == 0 {
		delete(live, LiveKeyUnconfirmed)
		return nil
	}
	raw, err := json.Marshal(m)
	if err == nil {
		live[LiveKeyUnconfirmed] = raw
	}
	return err
}

// linkedGroups возвращает связную компоненту участников group: в обе стороны.
func linkedGroups(items []Group, gid string) []string {
	seen := map[string]bool{gid: true}
	for changed := true; changed; {
		changed = false
		for _, g := range items {
			for _, m := range g.Members {
				if m.Kind != KindGroup || (!seen[g.ID] && !seen[m.ID]) {
					continue
				}
				if !seen[g.ID] || !seen[m.ID] {
					changed = true
				}
				seen[g.ID], seen[m.ID] = true, true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Виды идущей операции в отметке компоненты (Service.inflight).
const (
	// inflightSelect — выбор участника (Select).
	inflightSelect = "select"
	// inflightConfirm — «Повторить» (Confirm) и сверка маркеров при старте.
	inflightConfirm = "confirm"
)

// beginSelection отмечает компоненту comp идущей операцией вида kind. Хотя бы
// одна группа компоненты уже отмечена — ErrSelectBusy сразу, без ожидания и без
// очереди (D-17).
func (s *Service) beginSelection(comp []string, kind string) (func(), error) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	for _, id := range comp {
		if s.inflight[id] != "" {
			return nil, ErrSelectBusy
		}
	}
	if s.inflight == nil {
		s.inflight = map[string]string{}
	}
	for _, id := range comp {
		s.inflight[id] = kind
	}
	return func() {
		s.inflightMu.Lock()
		defer s.inflightMu.Unlock()
		for _, id := range comp {
			delete(s.inflight, id)
		}
	}, nil
}

// setInflightTarget запоминает цель выбора группы, уже поставленную в Mihomo
// ранним PUT быстрого пути: вид «Прокси» показывает её до записи Live фоновым
// проходом. Память процесса, не State.Live (выбор пишется только в Live).
func (s *Service) setInflightTarget(gid, member string) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if s.targets == nil {
		s.targets = map[string]string{}
	}
	s.targets[gid] = member
}

// clearInflightTarget снимает цель выбора группы: Live уже итоговый.
func (s *Service) clearInflightTarget(gid string) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	delete(s.targets, gid)
}

// inflightTargets — копия целей идущих выборов быстрого пути: ID группы →
// участник, уже поставленный в Mihomo и ещё не записанный в Live.
func (s *Service) inflightTargets() map[string]string {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	out := make(map[string]string, len(s.targets))
	for id, member := range s.targets {
		out[id] = member
	}
	return out
}

// inflightKinds — копия отметок идущих операций: ID группы → вид.
func (s *Service) inflightKinds() map[string]string {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	out := make(map[string]string, len(s.inflight))
	for id, kind := range s.inflight {
		out[id] = kind
	}
	return out
}

// selectionBlocked — выбор в группе groupID заблокирован маркером неизвестного
// исхода: маркер Xray стоит на всей компоненте comp (общий файл групп), маркер
// Mihomo — только на самой группе. Снимает его кнопка «Повторить».
func selectionBlocked(unconfirmed map[string]string, comp []string, groupID string) bool {
	for _, id := range comp {
		if k := unconfirmed[id]; k == configlayer.KernelXray || (id == groupID && k != "") {
			return true
		}
	}
	return false
}

// clearUnconfirmed снимает маркер kernel с групп comp. Запись состояния идёт на
// флешку, поэтому без маркера на comp ничего не пишется и не публикуется.
func (s *Service) clearUnconfirmed(comp []string, kernel string) error {
	if st, err := s.d.Layer.ReadState(); err == nil && !hasUnconfirmed(readUnconfirmed(st.Live), comp, kernel) {
		return nil
	}
	err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readUnconfirmed(live)
		for _, id := range comp {
			if m[id] == kernel {
				delete(m, id)
			}
		}
		return writeUnconfirmed(live, m)
	})
	if err == nil {
		s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	}
	return err
}

// hasUnconfirmed — хотя бы одна группа comp помечена маркером kernel.
func hasUnconfirmed(m map[string]string, comp []string, kernel string) bool {
	for _, id := range comp {
		if m[id] == kernel {
			return true
		}
	}
	return false
}

func selectionOK(out string) bool {
	return out == selApplied || out == selRestarted || out == selPendingStopped
}

func xraySelectionReverted(outcome string) bool {
	switch outcome {
	case configlayer.RestartOutcomeFailedRolledBack, configlayer.ResultWriteFailed,
		configlayer.ResultBuildFailed, configlayer.ResultBeforeWriteFailed,
		configlayer.ResultStateWriteFailed, configlayer.ResultDraftConflict,
		configlayer.CodeValidationFailed, configlayer.CodeValidationTimeout, configlayer.CodeValidationNotRun:
		return true
	}
	return false
}

// readRepick читает список групп с пропавшим выбранным узлом.
func readRepick(live configlayer.Sections) []string {
	raw, ok := live[LiveKeyRepick]
	if !ok || isEmptyRaw(raw) {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// writeRepick записывает список; пустой список удаляет ключ.
func writeRepick(live configlayer.Sections, ids []string) error {
	if len(ids) == 0 {
		delete(live, LiveKeyRepick)
		return nil
	}
	sort.Strings(ids)
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	live[LiveKeyRepick] = raw
	return nil
}

// writeSelection записывает выбор участников; пустая карта удаляет ключ.
func writeSelection(live configlayer.Sections, sel map[string]string) error {
	if len(sel) == 0 {
		delete(live, LiveKeySelection)
		return nil
	}
	raw, err := json.Marshal(sel)
	if err != nil {
		return err
	}
	live[LiveKeySelection] = raw
	return nil
}

// without возвращает список без значения.
func without(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// setSelection записывает выбор группы и снимает с неё маркер повторного выбора:
// ручной выбор пользователя не должен быть перекрыт заменой пропавшего узла.
// keepRepick — выбор ставит сам повторный выбор (selectSync): маркер снимет
// repickOne после поставленного выбора, иначе он остаётся для следующего применения.
func (s *Service) setSelection(gid, member string, pending []string, keepRepick bool) error {
	return s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		unconfirmed := readUnconfirmed(live)
		if unconfirmed[gid] != "" {
			return ErrSelectionUnconfirmed
		}
		sel := liveSelection(live)
		sel[gid] = member
		if err := writeSelection(live, sel); err != nil {
			return err
		}
		for _, id := range pending {
			unconfirmed[id] = configlayer.KernelXray
		}
		if err := writeUnconfirmed(live, unconfirmed); err != nil {
			return err
		}
		if ids := readRepick(live); len(ids) > 0 && !keepRepick {
			return writeRepick(live, without(ids, gid))
		}
		return nil
	})
}

// restoreSelection возвращает прежнее значение выбора, если никто не успел его
// сменить.
func (s *Service) restoreSelection(gid, member, prev string, had bool, comp []string) error {
	err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		sel := liveSelection(live)
		if sel[gid] != member {
			return nil
		}
		if had {
			sel[gid] = prev
		} else {
			delete(sel, gid)
		}
		if err := writeSelection(live, sel); err != nil {
			return err
		}
		m := readUnconfirmed(live)
		for _, id := range comp {
			if m[id] == configlayer.KernelXray {
				delete(m, id)
			}
		}
		return writeUnconfirmed(live, m)
	})
	if err != nil {
		log.Printf("[routing] выбор участника не возвращён: %v", err)
	}
	return err
}

// clearRepick снимает маркер повторного выбора с группы.
func (s *Service) clearRepick(gid string) {
	err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		ids := readRepick(live)
		if len(ids) == 0 {
			return nil
		}
		return writeRepick(live, without(ids, gid))
	})
	if err != nil {
		log.Printf("[routing] маркер повторного выбора не снят: %v", err)
	}
}

// markReconcile просит наблюдателя повторить сверку в ближайшем цикле, не
// дожидаясь нового процесса Mihomo.
func (s *Service) markReconcile() { s.reconcilePending.Store(true) }

// --- Select ---

// Select выбирает участника ручной группы без черновика (U-05): выбор пишется в
// Live, Mihomo получает его сразу (PUT /proxies), Xray — через запись xcp-groups и
// горячую замену с резервным перезапуском. Остановленное ядро не запускается: Xray
// прочитает файл при старте, Mihomo получит выбор сверкой при запуске процесса.
// Ревизия черновика не меняется.
//
// Быстрый путь (SC2): Mihomo запущен, Xray установлен и остановлен, маршрутизацией
// обоих ядер управляет панель. Выбор ставится в Mihomo до прохода файла Xray, ответ
// приходит сразу после Clash API, а xcp-groups дописывает фоновое продолжение той
// же пачки выбора (selectMihomoFirstTail): отметка компоненты (D-17) держится до
// его конца, возврат неудачного прохода — под ходом (CR-01), затем Mihomo получает
// выбор из Live. Ранний PUT не удался — прежний синхронный порядок; ядро могло
// принять PUT после таймаута клиента, поэтому сверка Mihomo запрошена, а выходы,
// где выбор не записан или возвращён, ставят Mihomo по Live (WR-02).
//
// Select — вход клика пользователя (обработчик): быстрый путь разрешён.
func (s *Service) Select(ctx context.Context, groupID, member string) (SelectResult, error) {
	return s.selectOpts(ctx, groupID, member, true)
}

// selectSync — служебный вход выбора без раннего ответа (WR-01 ревью 2): повторный
// выбор (selectWithRetry) не получает ответ до прохода файла Xray, его итог — маркер
// повторного выбора и уведомление routing_repick:, поэтому он ждёт исход прохода и
// постановку Mihomo по Live. Маркер повторного выбора группы проход не снимает:
// его снимает repickOne только после поставленного выбора.
func (s *Service) selectSync(ctx context.Context, groupID, member string) (SelectResult, error) {
	return s.selectOpts(ctx, groupID, member, false)
}

// selectOpts — тело выбора (Select, selectSync). fast — разрешён быстрый путь при
// запущенном Mihomo и остановленном Xray (ранний PUT и фоновый хвост); false —
// прежний синхронный порядок: проход файла Xray, затем Mihomo по Live, и маркер
// повторного выбора группы остаётся за вызывающим (keepRepick).
func (s *Service) selectOpts(ctx context.Context, groupID, member string, fast bool) (SelectResult, error) {
	start := time.Now()
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return SelectResult{}, err
	}
	desired := configlayer.DesiredRouting(st.Applied)
	if !desired.AnyManaged() {
		return SelectResult{}, ErrGroupNotApplied
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return SelectResult{}, err
	}
	g, ok := am.group(groupID)
	if !ok || g.Mode != ModeManual {
		return SelectResult{}, ErrGroupNotApplied
	}
	if !memberAllowed(g, member, am) {
		return SelectResult{}, ErrMemberInvalid
	}

	comp := linkedGroups(am.groups.Items, groupID)
	release, err := s.beginSelection(comp, inflightSelect)
	if err != nil {
		return SelectResult{}, err
	}
	model := time.Since(start)
	// Событие начала: другие вкладки перечитывают вид и видят переключение.
	// Событие конца — после снятия отметки, иначе перечитывание увидит группу
	// ещё занятой (D-14, D-17); уходит во всех исходах. handoff — отметку и событие
	// конца забрала фоновая задача быстрого пути.
	s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	handoff := false
	defer func() {
		if handoff {
			return
		}
		release()
		s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	}()
	// Повторное чтение после захвата компоненты исключает устаревший маркер. Снимок
	// модели остаётся для раннего PUT: при том же входе каталога цель считается по
	// уже собранной модели (mihomoEntryFromModel).
	modelSt := st
	st, err = s.d.Layer.ReadState()
	if err != nil {
		return SelectResult{}, err
	}
	// Маркер Xray ставится на всю компоненту (общий файл групп), маркер Mihomo —
	// только на группу со сбоем: выбор в Mihomo у каждой группы свой. Связанная
	// группа без своего маркера не показывает «Повторить», поэтому маркер Mihomo
	// другой группы её выбор не блокирует.
	if selectionBlocked(readUnconfirmed(st.Live), comp, groupID) {
		return SelectResult{}, ErrSelectionUnconfirmed
	}
	res := SelectResult{Member: member, Kernels: map[string]string{
		configlayer.KernelXray: selNotInstalled, configlayer.KernelMihomo: selNotInstalled,
	}}
	mihomoManaged := am.installed.Mihomo && desired.ManagedFor(configlayer.KernelMihomo)
	// earlyTried — ранний PUT отправлялся, но ответ ушёл синхронным путём: Mihomo
	// мог принять цель (в том числе после таймаута клиента, WR-02). Выходы, где
	// выбор в Live не записан или возвращён, и постановка выбора из Live после
	// успешного прохода ставят Mihomo по Live контекстом без отмены запроса: обрыв
	// соединения клиента не оставляет ядро на цели и не даёт группе ложный маркер.
	earlyTried := false
	if am.installed.Xray && desired.ManagedFor(configlayer.KernelXray) {
		// Намерение пишется в Live под замками применения до сборки (Prepare), в
		// одной пачке с выборами независимых групп, пришедшими за время идущего
		// запуска (G-150-3).
		in := &xrayIntent{gid: groupID, member: member, comp: comp, keepRepick: !fast}
		stages := newXrayStages(groupID, start, model, in)
		restoreMihomo := func() {
			if !earlyTried {
				return
			}
			// Сбой внутри сам взводит сверку (markReconcile).
			s.selMu.Lock()
			s.selectMihomoLocked(context.WithoutCancel(ctx), groupID)
			s.selMu.Unlock()
		}
		if fast && mihomoManaged && s.d.Running(configlayer.KernelMihomo) && !s.d.Running(configlayer.KernelXray) {
			clashStart := time.Now()
			// pre — от захвата компоненты до раннего PUT (повторное чтение состояния,
			// проверка маркеров, событие начала).
			stages.pre = clashStart.Sub(start) - model
			early, et := s.selectMihomoEarly(ctx, groupID, member, am, modelSt)
			stages.clash = time.Since(clashStart)
			stages.clashWait, stages.clashPlan, stages.clashPut = et.wait, et.plan, et.put
			if early == selApplied {
				// Цель видна виду «Прокси» до записи Live фоновым проходом; событие —
				// вторая вкладка перечитывает вид и видит выбор (D-14).
				s.setInflightTarget(groupID, member)
				s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
				stages.answered = time.Now()
				handoff = true
				if s.Go(func() { s.selectMihomoFirstTail(in, stages, g.Name, release) }) {
					res.Kernels[configlayer.KernelMihomo] = selApplied
					res.Kernels[configlayer.KernelXray] = selPendingStopped
					return res, nil
				}
				// Сервис останавливается: прежний синхронный порядок.
				handoff = false
				stages.answered = time.Time{}
				s.clearInflightTarget(groupID)
			}
			// Ранний PUT не удался (или сервис останавливается): прежний порядок,
			// исходы и маркеры как раньше; сверка запрошена в selectMihomoEarly.
			earlyTried = true
		}
		defer stages.log()
		b, err := s.runXray(ctx, xrayRunSelect, in)
		stages.b, stages.err = b, err
		if err != nil {
			restoreMihomo()
			return SelectResult{}, err
		}
		if in.err != nil {
			restoreMihomo()
			return SelectResult{}, in.err
		}
		if !in.wrote {
			// Выбор не записан (замок применения занят, слой остановлен): ни
			// возврата, ни маркеров; Mihomo после раннего PUT — по Live.
			restoreMihomo()
			if b.err != nil {
				return SelectResult{}, b.err
			}
			return SelectResult{}, ErrApplyBusy
		}
		out, reason, outcome, runErr := b.out, b.reason, b.outcome, b.err
		res.Kernels[configlayer.KernelXray], res.RestartReason = out, reason
		// Разбор исхода замеряется до строки этапов (defer выполняется раньше).
		markersStart := time.Now()
		defer func() { stages.markers = time.Since(markersStart) }()
		switch {
		case selectionOK(out) && runErr == nil:
			// Маркер снят записью коммита прохода (CommitLive); отдельная запись —
			// только если коммита не было (слой выключен посреди запуска).
			if !b.markersCommitted {
				if err := s.clearUnconfirmed(comp, configlayer.KernelXray); err != nil {
					return res, err
				}
			}
		case runErr != nil || xraySelectionReverted(outcome):
			// Возврат выбора уже выполнен под ходом (settleXrayBatch): исход прохода
			// один на пачку, возврат касается только своей группы и только при
			// неизменённом значении.
			if in.restoreErr != nil {
				res.Unconfirmed = comp
				return res, in.restoreErr
			}
			res.Reverted = true
			if mihomoManaged {
				res.Kernels[configlayer.KernelMihomo] = selFailed
				// Ранний PUT не подтвердился, но мог дойти до ядра: Mihomo получает
				// возвращённый выбор из Live.
				restoreMihomo()
			}
			return res, runErr
		default:
			// Неизвестный исход никогда не выдаётся за успешный возврат.
			res.Unconfirmed = comp
			if mihomoManaged {
				res.Kernels[configlayer.KernelMihomo] = selFailed
			}
			// Маркер без записи Prepare поставлен под ходом (settleXrayBatch).
			if in.markErr != nil {
				return res, in.markErr
			}
			return res, nil
		}
	} else {
		if err := s.setSelection(groupID, member, nil, !fast); err != nil {
			return SelectResult{}, err
		}
	}
	// Выбор записан в Live: прежнее уведомление об отказе группы снимается.
	s.selNotes.drop(s.d.Layer, groupID)
	if mihomoManaged {
		// Маркер и ответ фиксируются под тем же замком, что фоновая сверка. После
		// неудачного раннего PUT ядро могло принять цель: Mihomo ставится по Live
		// контекстом без отмены запроса (как restoreMihomo), иначе закрытая вкладка
		// дала бы группе маркер и «Повторить» при доступном ядре.
		mctx := ctx
		if earlyTried {
			mctx = context.WithoutCancel(ctx)
		}
		s.selMu.Lock()
		out, err := s.selectMihomoMarked(mctx, groupID)
		s.selMu.Unlock()
		res.Kernels[configlayer.KernelMihomo] = out
		if out == selFailed {
			res.Unconfirmed = []string{groupID}
		}
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// selectMihomoMarked ставит выбор группы из Live в Mihomo (selectMihomoLocked);
// сбой — маркер Mihomo на группе (D-16). Вызывается под selMu.
func (s *Service) selectMihomoMarked(ctx context.Context, gid string) (string, error) {
	out := s.selectMihomoLocked(ctx, gid)
	if out != selFailed {
		return out, nil
	}
	return out, s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readUnconfirmed(live)
		m[gid] = configlayer.KernelMihomo
		return writeUnconfirmed(live, m)
	})
}

// appliedOptions — варианты участника ручной группы g по применённому состоянию:
// участники группы и узлы её состава в каждом установленном ядре. g — группа из
// применённой модели am.
func appliedOptions(g Group, am appliedModel) []MemberOption {
	var xray, mihomo []sources.CatalogNode
	if am.installed.Xray {
		xray = Resolve(g, am.cat.Xray, am.groups.Items).Nodes
	}
	if am.installed.Mihomo {
		mihomo = Resolve(g, am.cat.Mihomo, am.groups.Items).Nodes
	}
	members := Resolve(g, am.union, am.groups.Items).Members
	return memberOptions(g, members, am.groups.Items, xray, mihomo)
}

// memberAllowed — участник есть среди вариантов группы по применённому состоянию
// (те же варианты, что отдаёт представление для применённой группы,
// appliedOptions). Выбор вне состава effectiveSelection всё равно игнорировал бы.
func memberAllowed(g Group, member string, am appliedModel) bool {
	for _, o := range appliedOptions(g, am) {
		if o.Member == member {
			return true
		}
	}
	return false
}

// xrayOutcome переводит итог запуска конвейера в исход выбора для Xray.
func (s *Service) xrayOutcome(view configlayer.ApplyView) (string, string, string) {
	// Общий итог приоритетнее строки рестарта: commitState мог вызвать откат
	// уже после успешного горячего пути.
	if view.Result == nil {
		return selFailed, "", configlayer.ResultInterrupted
	}
	if !view.Result.OK {
		if view.Result.Code == configlayer.ResultRestartFailed && view.Result.RolledBack {
			return selFailed, "", configlayer.RestartOutcomeFailedRolledBack
		}
		return selFailed, "", view.Result.Code
	}
	for _, r := range view.Restart {
		if r.Kernel != configlayer.KernelXray {
			continue
		}
		reason := ""
		switch r.NoteCode {
		case configlayer.HotReasonNoAPI:
			reason = "no_api"
		case configlayer.HotReasonVersion:
			reason = "version"
		case configlayer.HotReasonFailed:
			reason = "hot_failed"
		}
		switch r.Outcome {
		case configlayer.RestartOutcomeHotReloaded:
			return selApplied, "", r.Outcome
		case configlayer.RestartOutcomeRestarted, configlayer.RestartOutcomeRestartedReload:
			return selRestarted, reason, r.Outcome
		case configlayer.RestartOutcomeDeferred, configlayer.RestartOutcomeUntouchedIdle, configlayer.RestartOutcomeUntouchedClash:
			return selPendingStopped, "", r.Outcome
		case configlayer.RestartOutcomeNoRuntimeEffect:
			return selApplied, "", r.Outcome
		default:
			return selFailed, reason, r.Outcome
		}
	}
	if s.d.Running(configlayer.KernelXray) {
		return selApplied, "", ""
	}
	return selPendingStopped, "", ""
}

// mihomoEntryFor — ожидаемый выбор группы gid в профиле Mihomo, если её выбор в
// Live станет member: расчёт mihomoPlan по копии Live с этим выбором. nil — группа
// в профиле Mihomo не материализована. Расчёт чистый: ни Live, ни диск не меняются.
func (s *Service) mihomoEntryFor(st configlayer.StateView, installed configlayer.InstalledKernels, gid, member string) (*mihomoSelEntry, error) {
	live := make(configlayer.Sections, len(st.Live)+1)
	for k, v := range st.Live {
		live[k] = append(json.RawMessage(nil), v...)
	}
	sel := liveSelection(live)
	sel[gid] = member
	if err := writeSelection(live, sel); err != nil {
		return nil, err
	}
	st.Live = live
	plan, err := s.mihomoPlan(st, installed)
	if err != nil {
		return nil, err
	}
	for i := range plan {
		if plan[i].gid == gid {
			e := plan[i]
			return &e, nil
		}
	}
	return nil, nil
}

// mihomoEntryFromModel — то же, что mihomoEntryFor, но по группам и каталогу
// модели am, которую Select уже собрал (P1 разбора пробела 2): без второго
// Sources.Catalog, разбора групп, копии Live и расчёта выбора всех групп. Условия
// расчёта — как у mihomoBuildOf по снимку st; выбор группы gid — member, как в
// копии Live у mihomoEntryFor, остальные группы на цель не влияют. Вызывающий
// обязан убедиться, что вход каталога модели и снимка st один и тот же
// (catalogInputSame), иначе цель могла бы прийти из устаревшего профиля.
func (s *Service) mihomoEntryFromModel(am appliedModel, st configlayer.StateView, gid, member string) (*mihomoSelEntry, error) {
	if !s.mihomoSelPossible(st.Applied, am.installed) {
		return nil, nil
	}
	b := newMihomoBuild(mihomoBuild{groups: am.groups.Items, catalog: am.cat.Mihomo, groupsOK: true})
	b.prepare()
	for _, mg := range b.shapes {
		if mg.g.ID != gid {
			continue
		}
		if e, ok := b.selEntry(mg, member); ok {
			return &e, nil
		}
	}
	return nil, nil
}

// catalogInputSame сообщает, что вход каталога источников и расчёта профиля
// Mihomo у снимка cur тот же, что у снимка prev, по которому собрана модель am:
// те же установленные ядра и их версии (версионные ворота узлов каталога и групп
// Mihomo), то же применённое состояние (группы, секции источников, взятая
// маршрутизация) и тот же Live без ключей выбора групп (selection,
// selection_unconfirmed, selection_repick): каталог читает из Live только снимки
// подписок и кандидатов, а выбор групп в него не входит. Сравнение побайтное —
// любое расхождение, даже равнозначное, означает «не тот же», и цель считается
// прежним путём (mihomoEntryFor).
func (s *Service) catalogInputSame(am appliedModel, prev, cur configlayer.StateView) bool {
	if am.installed != s.d.Installed() ||
		am.xrayVer != s.d.KernelVersion(configlayer.KernelXray) ||
		am.mihomoVer != s.d.KernelVersion(configlayer.KernelMihomo) {
		return false
	}
	return sectionsSame(prev.Applied, cur.Applied, nil) &&
		sectionsSame(prev.Live, cur.Live, selectionLiveKeys)
}

// selectionLiveKeys — ключи Live, которые ведёт выбор групп; вход каталога и
// профиля Mihomo от них не зависит.
var selectionLiveKeys = map[string]bool{LiveKeySelection: true, LiveKeyUnconfirmed: true, LiveKeyRepick: true}

// sectionsSame — побайтное равенство секций a и b, кроме ключей skip.
func sectionsSame(a, b configlayer.Sections, skip map[string]bool) bool {
	n := 0
	for k, va := range a {
		if skip[k] {
			continue
		}
		vb, ok := b[k]
		if !ok || !bytes.Equal(va, vb) {
			return false
		}
		n++
	}
	for k := range b {
		if !skip[k] {
			n--
		}
	}
	return n == 0
}

// earlyTiming — части времени раннего PUT для строки этапов (разбор пробела 2):
// wait — ожидание selMu; plan — под замком до вызова Clash API и после него
// (чтение состояния, расчёт цели mihomoEntryFromModel или mihomoEntryFor); put —
// сам вызов SelectProxy (без вызова — 0). Сумма — поле clash строки этапов без
// накладных расходов замера.
type earlyTiming struct {
	wait, plan, put time.Duration
}

// selectMihomoEarly — ранний PUT быстрого пути: ставит в запущенный Mihomo выбор,
// который фоновый проход ещё запишет в Live. Под selMu, как сверка: сверка не
// перетирает выбор между расчётом и вызовом. Не applied — вызывающий идёт прежним
// синхронным порядком. Вместе с исходом — замер частей (earlyTiming); поведение
// выбора замер не меняет. Цель считается по модели am, собранной Select по
// снимку modelSt, если вход каталога со своим чтением тот же (catalogInputSame);
// иначе — прежним расчётом по своему чтению (mihomoEntryFor). Цель в обоих
// случаях одна и та же.
func (s *Service) selectMihomoEarly(ctx context.Context, gid, member string, am appliedModel, modelSt configlayer.StateView) (out string, tm earlyTiming) {
	waitStart := time.Now()
	s.selMu.Lock()
	defer s.selMu.Unlock()
	locked := time.Now()
	tm.wait = locked.Sub(waitStart)
	// plan — всё время под замком, кроме вызова Clash API; отложенный вызов идёт до
	// снятия замка (порядок defer обратный).
	defer func() { tm.plan = time.Since(locked) - tm.put }()
	if !s.d.Running(configlayer.KernelMihomo) {
		return selPendingStopped, tm
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return selFailed, tm
	}
	var entry *mihomoSelEntry
	if s.catalogInputSame(am, modelSt, st) {
		entry, err = s.mihomoEntryFromModel(am, st, gid, member)
	} else {
		entry, err = s.mihomoEntryFor(st, s.d.Installed(), gid, member)
	}
	if err != nil {
		return selFailed, tm
	}
	if entry == nil {
		// Группа в профиле Mihomo не материализована: выбирать там нечего.
		return selApplied, tm
	}
	if s.d.Mihomo == nil || !s.d.Mihomo.HasController() {
		return selFailed, tm
	}
	cctx, cancel := context.WithTimeout(ctx, reconcileCallTimeout)
	defer cancel()
	putStart := time.Now()
	err = s.d.Mihomo.SelectProxy(cctx, entry.group, entry.want)
	tm.put = time.Since(putStart)
	if err != nil {
		// PUT мог дойти до ядра (принят после таймаута клиента, WR-02): сверка
		// вернёт Mihomo к Live ближайшим циклом наблюдателя.
		log.Printf("[routing] ранний выбор Mihomo не поставлен: группа=%s Clash API: %s", utils.SanitizeLogInput(gid), clashErrClass(err))
		s.markReconcile()
		return selFailed, tm
	}
	return selApplied, tm
}

// selectMihomoFirstTail — фоновое продолжение быстрого пути после ответа: проход
// файла Xray той же пачкой выбора (Prepare пишет намерение в Live под замками
// применения, возврат неудачного прохода — settleXrayBatch под ходом, CR-01), снятие
// цели вида, затем Mihomo получает выбор из Live под selMu (после возврата — прежний
// узел), затем уведомление об отказе с фактическим исходом возврата (reverted,
// WR-03); успешный проход с Live, равным цели раннего PUT, повтор не делает
// (earlyHolds, код reput в строке этапов). В конце, в любом
// исходе, — снятие отметки компоненты, событие конца (D-14, D-17) и строка этапов.
func (s *Service) selectMihomoFirstTail(in *xrayIntent, stages *xrayStages, name string, release func()) {
	tailStart := time.Now()
	defer func() {
		release()
		s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
		stages.tail = time.Since(tailStart)
		stages.log()
	}()
	b, err := s.runXray(s.ctx, xrayRunSelect, in)
	// Live уже итоговый (записан Prepare или возвращён под ходом): вид берёт выбор
	// из Live. Окно до события конца закрывает само событие.
	s.clearInflightTarget(in.gid)
	stages.b, stages.err = b, err
	markersStart := time.Now()
	defer func() { stages.markers = time.Since(markersStart) }()

	var failErr error
	failCode := ""
	passOK := false
	// liveKept — Live группы прежний: выбор не записан или возвращён без сбоя
	// записи. Признак reverted уведомления отказа (WR-03) истинен, только если ещё и
	// Mihomo принял выбор из Live.
	liveKept := true
	switch {
	case err != nil:
		failErr = err
	case in.err != nil:
		failErr = in.err
	case !in.wrote:
		// Выбор не записан (замок применения занят, слой остановлен): Live прежний.
		failErr = b.err
		if failErr == nil {
			failErr = ErrApplyBusy
		}
	case selectionOK(b.out) && b.err == nil:
		passOK = true
		// Маркер снят записью коммита прохода (CommitLive), без коммита — здесь.
		if !b.markersCommitted {
			if err := s.clearUnconfirmed(in.comp, configlayer.KernelXray); err != nil {
				log.Printf("[routing] выбор группы %s: маркер Xray не снят: %v", utils.SanitizeLogInput(in.gid), utils.SanitizeLogInput(err.Error()))
			}
		}
		s.selNotes.drop(s.d.Layer, in.gid)
	case b.err != nil || xraySelectionReverted(b.outcome):
		// Возврат уже выполнен под ходом (settleXrayBatch).
		failErr, failCode = b.err, b.outcome
		if in.restoreErr != nil {
			// Возврат Live не записан: в Live остался отвергнутый выбор.
			failErr, failCode = in.restoreErr, ""
			liveKept = false
		}
	default:
		// Неизвестный исход: маркер Xray поставлен под ходом, D-16 показывает
		// «Повторить». Уведомление — только если маркер не записан; Live несёт новый
		// выбор, поэтому возвратом это не считается.
		failErr = in.markErr
		liveKept = false
	}

	// Mihomo получает выбор из Live под selMu. earlyHolds — проход успешен и выбор
	// группы в Live равен цели раннего PUT: значение уже подтверждено ядром (ранний
	// PUT ответил успехом под selMu), его повтор под остановкой носителя только
	// ставил бы ложный маркер D-16 группе, которой ответили «применено» (истина 35).
	// Вмешательство за время хвоста (новый процесс Mihomo, «Применить») доводит
	// сверка: пропустив группу с идущим выбором, она запрашивает повтор. Возврат,
	// неизвестный исход и Live ≠ цели — Mihomo по Live, сбой — маркер Mihomo (D-16).
	s.selMu.Lock()
	earlyHolds := false
	if passOK {
		if st, err := s.d.Layer.ReadState(); err == nil {
			earlyHolds = liveSelection(st.Live)[in.gid] == in.member
		}
	}
	mihomoOK := true
	stopping := s.ctx.Err() != nil
	switch {
	case earlyHolds:
		stages.reput = reputSkip
	case stopping:
		// Сервис останавливается: PUT отменённым контекстом не дойдёт до ядра, а
		// маркер Mihomo на флешке заблокировал бы группу после запуска. Mihomo
		// приводит к Live сверка старта наблюдателя (SelectionWatcher.tick: новый
		// PID ставит pending); флаг reconcilePending живёт в памяти и пережил бы
		// остановку только вместе с процессом, поэтому здесь он не взводится.
		stages.reput = reputDone
	default:
		stages.reput = reputDone
		out, err := s.selectMihomoMarked(s.ctx, in.gid)
		mihomoOK = out != selFailed
		if err != nil {
			log.Printf("[routing] выбор группы %s: маркер Mihomo не записан: %v", utils.SanitizeLogInput(in.gid), utils.SanitizeLogInput(err.Error()))
		}
	}
	s.selMu.Unlock()

	// Уведомление отказа — после постановки Mihomo по Live (порядок «Live → Mihomo →
	// уведомление»): признак reverted говорит, вернулся ли выбор на самом деле (WR-03).
	// Ложь — возврат Live не записан, исход неизвестен или Mihomo не принял выбор из
	// Live (маркер Mihomo, D-16): текст «Прежний узел возвращён» тогда был бы ложью.
	// При остановке уведомление reason=stopped не публикуется: исход доводит
	// сверка после запуска, а не пользователь.
	if (failErr != nil || failCode != "") && !stopping {
		code, reason := selectFailClass(failErr, failCode)
		s.noticeSelectFailed(in.gid, name, code, reason, liveKept && mihomoOK)
	}
}

// selectFailClass — код исхода и класс причины отказа фонового прохода для
// уведомления routing_select: rejected — отказ проверки, сборки, записи, откат или
// дрейф; busy — замок применения или операция с ядром; stopped — слой или сервис
// остановлен; failed — прочее.
func selectFailClass(err error, outcome string) (code, reason string) {
	switch {
	case errors.Is(err, ErrApplyBusy):
		return "apply_busy", selReasonBusy
	case errors.Is(err, configlayer.ErrKernelBusy):
		return "kernel_op_in_progress", selReasonBusy
	case errors.Is(err, configlayer.ErrStopped), errors.Is(err, context.Canceled):
		return "stopped", selReasonStopped
	case errors.Is(err, configlayer.ErrDriftBlocked):
		return "drift_blocked", selReasonRejected
	case errors.Is(err, configlayer.ErrDraftConflict):
		return configlayer.ResultDraftConflict, selReasonRejected
	case errors.Is(err, ErrSelectionUnconfirmed):
		return "selection_unconfirmed", selReasonFailed
	case outcome != "" && xraySelectionReverted(outcome):
		return outcome, selReasonRejected
	case outcome != "":
		return outcome, selReasonFailed
	}
	return selFailed, selReasonFailed
}

// noticeSelectFailed публикует уведомление отказа фонового прохода группы gid с
// новым идентификатором routing_select:<gid>:<n>: каждый отказ — новое уведомление
// (D-11), прежнее уведомление группы заменяется. В параметрах — ID и имя группы, код
// исхода, класс причины и reverted ("true" — Live и Mihomo на прежнем выборе,
// "false" — возврат не подтверждён, WR-03); имён узлов и адресов нет.
func (s *Service) noticeSelectFailed(gid, name, code, reason string, reverted bool) {
	n := s.selNoticeSeq.Add(1)
	s.selNotes.put(s.d.Layer, gid, configlayer.NoticeView{
		ID: noticeSelectPrefix + gid + ":" + strconv.FormatUint(n, 10), Kind: noticeWarning,
		Params: map[string]string{
			"gid": gid, "group": name, "code": code, "reason": reason,
			"reverted": strconv.FormatBool(reverted),
		},
	})
	log.Printf("[routing] выбор группы %s не записан в файл Xray: code=%s reason=%s reverted=%t", utils.SanitizeLogInput(gid), utils.SanitizeLogInput(code), utils.SanitizeLogInput(reason), reverted)
}

func (s *Service) selectMihomoLocked(ctx context.Context, gid string) string {
	if !s.d.Running(configlayer.KernelMihomo) {
		return selPendingStopped
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		log.Printf("[routing] выбор Mihomo не поставлен: группа=%s состояние слоя не прочитано", utils.SanitizeLogInput(gid))
		s.markReconcile()
		return selFailed
	}
	plan, err := s.mihomoPlan(st, s.d.Installed())
	if err != nil {
		log.Printf("[routing] выбор Mihomo не поставлен: группа=%s профиль не рассчитан", utils.SanitizeLogInput(gid))
		s.markReconcile()
		return selFailed
	}
	var entry *mihomoSelEntry
	for i := range plan {
		if plan[i].gid == gid {
			entry = &plan[i]
		}
	}
	if entry == nil {
		// Группа в профиле Mihomo не материализована: выбирать там нечего.
		return selApplied
	}
	if s.d.Mihomo == nil || !s.d.Mihomo.HasController() {
		log.Printf("[routing] выбор Mihomo не поставлен: группа=%s нет контроллера Clash API", utils.SanitizeLogInput(gid))
		s.markReconcile()
		return selFailed
	}
	cctx, cancel := context.WithTimeout(ctx, reconcileCallTimeout)
	defer cancel()
	if err := s.d.Mihomo.SelectProxy(cctx, entry.group, entry.want); err != nil {
		log.Printf("[routing] выбор Mihomo не поставлен: группа=%s Clash API: %s", utils.SanitizeLogInput(gid), clashErrClass(err))
		s.markReconcile()
		return selFailed
	}
	return selApplied
}

// clashErrClass — класс ошибки Clash API для журнала без имён и адресов: код
// ответа ядра, таймаут или сбой соединения.
func clashErrClass(err error) string {
	var status *services.ClashStatusError
	switch {
	case errors.As(err, &status):
		return "ответ " + strconv.Itoa(status.Status)
	case errors.Is(err, context.DeadlineExceeded):
		return "таймаут"
	default:
		return "сбой соединения"
	}
}

// --- выбор в профиле Mihomo ---

// ConfirmResult — подтверждение текущего намерения без нового выбора.
type ConfirmResult struct {
	Group     string `json:"group"`
	Confirmed bool   `json:"confirmed"`
}

func (s *Service) Confirm(ctx context.Context, groupID string) (ConfirmResult, error) {
	res := ConfirmResult{Group: groupID}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return res, err
	}
	groups, err := parseGroups(st.Applied[GroupsSection])
	if err != nil {
		return res, err
	}
	comp := linkedGroups(groups.Items, groupID)
	release, err := s.beginSelection(comp, inflightConfirm)
	if err != nil {
		return res, err
	}
	// События начала и конца — как у Select: конец после снятия отметки.
	s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	defer func() {
		release()
		s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	}()
	st, err = s.d.Layer.ReadState()
	if err != nil {
		return res, err
	}
	kernel := readUnconfirmed(st.Live)[groupID]
	if kernel == "" {
		res.Confirmed = true
		return res, nil
	}
	if kernel == configlayer.KernelXray {
		// Тот же порядок запусков, что у выбора: во время выбора независимой
		// группы «Повторить» ждёт его конца, а не отвечает apply_busy.
		b, err := s.runXray(ctx, xrayRunConfirm, nil)
		if err != nil || b.err != nil || !selectionOK(b.out) {
			return res, ErrConfirmFailed
		}
		if err := s.clearUnconfirmed(comp, kernel); err != nil {
			return res, err
		}
	} else if kernel == configlayer.KernelMihomo {
		s.selMu.Lock()
		out := s.selectMihomoLocked(ctx, groupID)
		if !selectionOK(out) {
			s.selMu.Unlock()
			return res, ErrConfirmFailed
		}
		err := s.clearUnconfirmed([]string{groupID}, kernel)
		s.selMu.Unlock()
		if err != nil {
			return res, err
		}
	} else {
		return res, ErrConfirmFailed
	}
	res.Confirmed = true
	return res, nil
}

// reconcileXray повторно применяет сохранённое намерение после остановки панели.
func (s *Service) reconcileXray(ctx context.Context) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	groups, err := parseGroups(st.Applied[GroupsSection])
	if err != nil {
		return
	}
	var ids []string
	for id, kernel := range readUnconfirmed(st.Live) {
		if kernel == configlayer.KernelXray {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	locked := map[string]bool{}
	for _, id := range ids {
		for _, linked := range linkedGroups(groups.Items, id) {
			locked[linked] = true
		}
	}
	comp := make([]string, 0, len(locked))
	for id := range locked {
		comp = append(comp, id)
	}
	release, err := s.beginSelection(comp, inflightConfirm)
	if err != nil {
		return
	}
	// Как в Confirm: события начала и конца, иначе вкладка, перечитавшая вид во время
	// сверки, остаётся с заблокированными плитками (switching без события конца).
	s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	defer func() {
		release()
		s.d.Layer.PublishProxies(configlayer.ProxiesKindSelect)
	}()
	b, err := s.runXray(ctx, xrayRunConfirm, nil)
	if err == nil && b.err == nil && selectionOK(b.out) {
		_ = s.clearUnconfirmed(ids, configlayer.KernelXray)
	}
}

// mihomoSelEntry — ожидаемый выбор ручной группы в профиле Mihomo.
type mihomoSelEntry struct {
	gid, group, want string
}

// mihomoBuildOf готовит расчёт профиля Mihomo по применённому состоянию, ровно
// как генератор (имена групп и копий узлов). nil — Mihomo не установлен,
// маршрутизацией не управляет панель или группы ниже порога версии (D-45).
func (s *Service) mihomoBuildOf(st configlayer.StateView, installed configlayer.InstalledKernels) (*mihomoBuild, error) {
	if !s.mihomoSelPossible(st.Applied, installed) {
		return nil, nil
	}
	groups, err := parseGroups(st.Applied[GroupsSection])
	if err != nil {
		return nil, err
	}
	cat, err := s.d.Sources.Catalog(appliedInput(st), installed)
	if err != nil {
		return nil, err
	}
	b := newMihomoBuild(mihomoBuild{groups: groups.Items, catalog: cat.Mihomo, groupsOK: true})
	b.prepare()
	return b, nil
}

// mihomoSelPossible — выбор групп в профиле Mihomo есть: Mihomo установлен,
// маршрутизацией Mihomo управляет панель и группы не ниже порога версии (D-45).
func (s *Service) mihomoSelPossible(applied configlayer.Sections, installed configlayer.InstalledKernels) bool {
	return installed.Mihomo && configlayer.DesiredRouting(applied).ManagedFor(configlayer.KernelMihomo) && s.mihomoGroupsAvailable()
}

// nodeNames — имя в профиле Mihomo для каждого узла состава группы. У выпущенной
// ручной группы имена берутся из её формы (shapeByID) — той же, что пишет
// генератор: имя копии (отдельные узлы Picks и все узлы группы при copyAll) или
// имя в файле провайдера. Иначе (группа не выпущена, группы «авто» и «резерв») —
// имя копии у отдельных узлов (picks) и имя в файле провайдера у остальных.
func (b *mihomoBuild) nodeNames(g Group) map[string]string {
	if mg := b.shapeByID[g.ID]; mg != nil && len(mg.names) > 0 {
		out := make(map[string]string, len(mg.names))
		for ref, name := range mg.names {
			out[ref] = name
		}
		return out
	}
	out := map[string]string{}
	base, picks := b.split(g)
	for _, n := range base {
		out[n.Ref] = n.MihomoName
	}
	for _, n := range picks {
		out[n.Ref] = b.copyName[n.Ref]
	}
	return out
}

// memberName — имя участника в группе профиля Mihomo; пусто — участник в
// профиле отсутствует.
func (b *mihomoBuild) memberName(member string, names map[string]string) string {
	switch {
	case member == memberDirect:
		return mihomoDirect
	case member == memberReject:
		return mihomoReject
	case strings.HasPrefix(member, memberGroup):
		id := strings.TrimPrefix(member, memberGroup)
		if g := b.byID[id]; g != nil && b.isEmitted(id) {
			return g.Name
		}
	case strings.HasPrefix(member, memberNode):
		return names[strings.TrimPrefix(member, memberNode)]
	}
	return ""
}

// mihomoPlan считает ожидаемый выбор каждой материализованной ручной группы по
// Live и effectiveSelection. Пусто — сверять нечего. У каждой выпущенной ручной
// группы выбор по умолчанию непуст по самому правилу выпуска (manualPick, D-36),
// поэтому Mihomo не остаётся на DIRECT, который пользователь не ставил первым;
// ветка pick == "" — защита, сверку для такой группы она не делает.
func (s *Service) mihomoPlan(st configlayer.StateView, installed configlayer.InstalledKernels) ([]mihomoSelEntry, error) {
	b, err := s.mihomoBuildOf(st, installed)
	if err != nil || b == nil {
		return nil, err
	}
	live := liveSelection(st.Live)
	var out []mihomoSelEntry
	for _, mg := range b.shapes {
		if e, ok := b.selEntry(mg, live[mg.g.ID]); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// selEntry — ожидаемый выбор материализованной группы mg при выборе sel в Live:
// effectiveSelection по именам профиля Mihomo. false — группа не ручная или
// допустимого участника нет (pick == "", защита).
func (b *mihomoBuild) selEntry(mg *mihomoGroup, sel string) (mihomoSelEntry, bool) {
	g := mg.g
	if g.Mode != ModeManual {
		return mihomoSelEntry{}, false
	}
	names := b.nodeNames(g)
	pick := effectiveSelection(g, sel, func(m string) bool { return b.memberName(m, names) != "" },
		nodeRefs(b.res[g.ID].Nodes)...)
	if pick == "" {
		return mihomoSelEntry{}, false
	}
	return mihomoSelEntry{gid: g.ID, group: g.Name, want: b.memberName(pick, names)}, true
}

// reconcileMihomo сверяет now ручных групп Mihomo с выбором из State.Live и
// ставит выбор панели через PUT /proxies/<группа>, если они расходятся
// (источник истины — панель; store-selected восстанавливает из cache.db прежний
// выбор процесса). Общая сверка для AfterApply и SelectionWatcher; идёт под тем же
// замком selMu, что ветка Mihomo в Select. Сбой по группе — уведомление
// routing_reconcile:<gid>, успех снимает его. Нет ответа Clash API (контроллер ещё
// не поднялся, процесс ещё не признан запущенным) — errReconcileRetry: вызывающий
// повторяет позже. Сверять нечего (Mihomo не установлен, маршрутизация не взята,
// группы не материализованы, дрейф профиля) — nil.
func (s *Service) reconcileMihomo(ctx context.Context) error {
	s.selMu.Lock()
	defer s.selMu.Unlock()

	installed := s.d.Installed()
	if !installed.Mihomo || s.d.Mihomo == nil {
		return nil
	}
	if !s.d.Running(configlayer.KernelMihomo) {
		// Процесс, который увидел наблюдатель, источник состояния ещё не признал
		// запущенным: сверку надо повторить, а не считать выполненной.
		return errReconcileRetry
	}
	snap := s.d.Layer.Snapshot()
	if mh := snap.Routing.Mihomo; mh == nil || !mh.Taken || len(mh.Drift) > 0 {
		s.setReconcileNotices(nil)
		return nil
	}
	if !s.d.Mihomo.HasController() {
		return nil
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return nil
	}
	plan, err := s.mihomoPlan(st, installed)
	if err != nil {
		log.Printf("[routing] сверка выбора Mihomo пропущена: профиль не рассчитан")
		return nil
	}
	if len(plan) == 0 {
		s.setReconcileNotices(nil)
		return nil
	}
	var failed []mihomoSelEntry
	var confirmed []string
	// Маркеры подтверждённых групп снимаются одной записью и при раннем выходе:
	// поставленный выбор не должен остаться «неподтверждённым».
	defer func() {
		if len(confirmed) == 0 {
			return
		}
		if err := s.clearUnconfirmed(confirmed, configlayer.KernelMihomo); err != nil {
			log.Printf("[routing] сверка Mihomo: маркер неподтверждённого выбора не снят: %v", err)
		}
	}()
	// Группы с идущей операцией пропускаются: быстрый путь выбора уже поставил
	// новый узел в Mihomo ранним PUT, а Live его запишет фоновый проход позже —
	// сверка в этом окне вернула бы прежний узел. Выбор таких групп ставит владелец
	// отметки по окончании прохода (под selMu, из Live; успешный быстрый путь
	// повтор не делает, earlyHolds); их маркеры и уведомления сверки остаются как
	// есть, а сверка запрашивает повтор: вмешательство за время прохода (новый
	// процесс Mihomo, «Применить») доводит следующий цикл наблюдателя после снятия
	// отметки.
	busy := s.inflightKinds()
	noted := map[string]bool{}
	for _, n := range snap.Notices {
		if strings.HasPrefix(n.ID, noticeReconcilePrefix) {
			noted[strings.TrimPrefix(n.ID, noticeReconcilePrefix)] = true
		}
	}
	for _, e := range plan {
		if busy[e.gid] != "" {
			s.markReconcile()
			// Уведомление сверки группы сохраняется: набор заменяется целиком.
			if noted[e.gid] {
				failed = append(failed, e)
			}
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, reconcileCallTimeout)
		now, err := s.d.Mihomo.ProxyNow(cctx, e.group)
		if err == nil && now != e.want {
			err = s.d.Mihomo.SelectProxy(cctx, e.group, e.want)
		}
		cancel()
		if err == nil {
			confirmed = append(confirmed, e.gid)
			continue
		}
		var status *services.ClashStatusError
		if errors.As(err, &status) {
			failed = append(failed, e)
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errReconcileRetry
	}
	s.setReconcileNotices(failed)
	return nil
}

// failReconcile сообщает по каждой ручной группе Mihomo, что выбор не поставлен:
// Clash API не ответил за отведённое время.
func (s *Service) failReconcile() {
	s.selMu.Lock()
	defer s.selMu.Unlock()
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	plan, err := s.mihomoPlan(st, s.d.Installed())
	if err != nil {
		return
	}
	s.setReconcileNotices(plan)
}

// setReconcileNotices заменяет уведомления routing_reconcile списком групп;
// пустой список снимает их.
func (s *Service) setReconcileNotices(failed []mihomoSelEntry) {
	list := make([]configlayer.NoticeView, 0, len(failed))
	for _, e := range failed {
		list = append(list, configlayer.NoticeView{
			ID: noticeReconcilePrefix + e.gid, Kind: noticeWarning, Kernel: configlayer.KernelMihomo,
			Params: map[string]string{"group": e.group},
		})
	}
	s.d.Layer.SetNotices(noticeReconcilePrefix, list)
}

// --- фиксация после применения ---

// CommitLive — хук фиксации живых данных для Layer.AddLiveCommitter. Вызывается
// внутри записи состояния: пинга и сети здесь нет. Для каждой ручной группы
// выбор, указывающий на участника, которого больше нет (узел пропал из каталога
// применённых данных, группа удалена, узел вышел из состава), заменяется
// детерминированным выбором effectiveSelection (тем же, что уже использовал
// генератор), а группа попадает в LiveKeyRepick: AfterApply заменит его первым
// живым по пингу. Выбор удалённых групп и участников-групп, DIRECT/REJECT,
// которых в составе больше нет, снимается молча и в повторный выбор не идёт:
// это правка пользователя, а не пропавший узел.
//
// Запуск выбора Xray (TriggerSelect) сначала, до ранних возвратов, снимает маркеры
// selection_unconfirmed Xray с групп компонент своих записанных намерений
// (xrayRunGate.commitComp): запись коммита идёт только после успешного
// перезапуска, отдельной записи после ответа нет (150-26, IO=B). Отказ коммита
// отменяет и снятие; неизвестный исход ставит маркер снова (settleXrayBatch).
func (s *Service) CommitLive(st *configlayer.State, req configlayer.ApplyRequest, used configlayer.Input) error {
	if req.Trigger == configlayer.TriggerSelect {
		g := &s.xrun
		g.mu.Lock()
		comp := g.commitComp
		g.mu.Unlock()
		if len(comp) > 0 {
			if st.Live == nil {
				st.Live = configlayer.Sections{}
			}
			m := readUnconfirmed(st.Live)
			for _, id := range comp {
				if m[id] == configlayer.KernelXray {
					delete(m, id)
				}
			}
			if err := writeUnconfirmed(st.Live, m); err != nil {
				return err
			}
			g.mu.Lock()
			g.committed = true
			g.mu.Unlock()
		}
	}
	fullDraft := req.Source == configlayer.SourceDraft && len(req.Only) == 0
	if !fullDraft && req.Source != configlayer.SourceApplied {
		return nil
	}
	cfg, err := parseGroups(used.Sections[GroupsSection])
	if err != nil {
		// Испорченную секцию сообщает проверка; выбор остаётся как есть.
		return nil
	}
	if st.Live == nil {
		st.Live = configlayer.Sections{}
	}
	sel := liveSelection(st.Live)
	repick := readRepick(st.Live)
	if len(sel) == 0 && len(repick) == 0 {
		return nil
	}

	manual := map[string]Group{}
	for _, g := range cfg.Items {
		if g.Mode == ModeManual {
			manual[g.ID] = g
		}
	}
	for gid := range sel {
		if _, ok := manual[gid]; !ok {
			delete(sel, gid)
		}
	}
	keep := repick[:0:0]
	for _, gid := range repick {
		if _, ok := manual[gid]; ok {
			keep = append(keep, gid)
		}
	}
	repick = keep

	installed := s.d.Installed()
	if installed.Xray || installed.Mihomo {
		if cat, err := s.d.Sources.Catalog(used, installed); err == nil {
			am := appliedModel{groups: cfg, cat: cat, union: unionCatalog(cat), installed: installed, byRef: map[string]sources.CatalogNode{}}
			for _, n := range am.union {
				am.byRef[n.Ref] = n
			}
			for gid, cur := range sel {
				g := manual[gid]
				cands := am.candidates(g)
				if cur != "" && containsString(cands, cur) && am.avail(cur) {
					continue
				}
				if cur != "" && !strings.HasPrefix(cur, memberNode) {
					// Участник-группа удалена или опустела, DIRECT/REJECT убран из
					// состава: это правка пользователя, а не пропавший узел. Выбор
					// снимается молча, дальше действует выбор по умолчанию.
					delete(sel, gid)
					continue
				}
				refs := make([]string, 0, len(cands))
				for _, c := range cands {
					if ref, ok := strings.CutPrefix(c, memberNode); ok {
						refs = append(refs, ref)
					}
				}
				next := effectiveSelection(g, "", am.avail, refs...)
				if next == "" {
					delete(sel, gid)
					continue
				}
				sel[gid] = next
				if !containsString(repick, gid) {
					repick = append(repick, gid)
				}
			}
		}
	}
	if err := writeSelection(st.Live, sel); err != nil {
		return err
	}
	return writeRepick(st.Live, repick)
}

// containsString сообщает, что значение есть в списке.
func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// --- после применения ---

// AfterApply — хук Layer.AddAfterApply: сверяет now групп Mihomo с выбором панели
// (кроме запусков самого выбора: Select уже поставил его) и заменяет пропавшие
// узлы первыми живыми по пингу (D-36).
func (s *Service) AfterApply(ctx context.Context, info configlayer.AfterApplyInfo) {
	defer s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
	if info.Trigger != configlayer.TriggerSelect {
		if err := s.reconcileMihomo(ctx); errors.Is(err, errReconcileRetry) {
			s.markReconcile()
		}
	}
	s.repickAll(ctx)
}

// groupNotes — уведомления одного префикса по группам (routing_repick,
// routing_select): одно на группу, хранятся, пока группа существует, потому что
// SetNotices заменяет набор префикса целиком. prefix задаётся при создании сервиса.
type groupNotes struct {
	prefix string
	mu     sync.Mutex
	notes  map[string]configlayer.NoticeView
}

// put сохраняет уведомление группы и заменяет набор в слое. Закрытое
// пользователем уведомление той же группы возвращается новым событием: набор
// сначала обновляется без него.
func (r *groupNotes) put(l *configlayer.Layer, gid string, n configlayer.NoticeView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.notes == nil {
		r.notes = map[string]configlayer.NoticeView{}
	}
	if _, had := r.notes[gid]; had {
		delete(r.notes, gid)
		l.SetNotices(r.prefix, r.listLocked())
	}
	r.notes[gid] = n
	l.SetNotices(r.prefix, r.listLocked())
}

// drop снимает уведомление группы; нет уведомления — набор в слое не трогается.
func (r *groupNotes) drop(l *configlayer.Layer, gid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, had := r.notes[gid]; !had {
		return
	}
	delete(r.notes, gid)
	l.SetNotices(r.prefix, r.listLocked())
}

// prune убирает уведомления групп, которых больше нет.
func (r *groupNotes) prune(l *configlayer.Layer, exists func(gid string) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for gid := range r.notes {
		if !exists(gid) {
			delete(r.notes, gid)
			changed = true
		}
	}
	if changed {
		l.SetNotices(r.prefix, r.listLocked())
	}
}

func (r *groupNotes) listLocked() []configlayer.NoticeView {
	out := make([]configlayer.NoticeView, 0, len(r.notes))
	for _, n := range r.notes {
		out = append(out, n)
	}
	return out
}

// repickAll обрабатывает группы с маркером LiveKeyRepick. Один проход за раз:
// выбор, который ставит сам проход, зовёт AfterApply с TriggerSelect, а не новый
// проход.
func (s *Service) repickAll(ctx context.Context) {
	if !s.repickMu.TryLock() {
		return
	}
	defer s.repickMu.Unlock()

	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	if cfg, err := parseGroups(st.Applied[GroupsSection]); err == nil {
		ids := map[string]bool{}
		for _, g := range cfg.Items {
			ids[g.ID] = true
		}
		s.repick.prune(s.d.Layer, func(gid string) bool { return ids[gid] })
		s.selNotes.prune(s.d.Layer, func(gid string) bool { return ids[gid] })
	}
	for _, gid := range readRepick(st.Live) {
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return
		}
		s.repickOne(ctx, gid)
	}
}

// repickOne заменяет пропавший выбранный узел группы первым живым.
func (s *Service) repickOne(ctx context.Context, gid string) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return
	}
	g, ok := am.group(gid)
	if !ok || g.Mode != ModeManual {
		s.clearRepick(gid)
		return
	}
	// Ручной выбор упёрся бы в тот же маркер неизвестного исхода: пачку замера не
	// гоняем, маркер повторного выбора остаётся до «Повторить» и следующего запуска.
	if selectionBlocked(readUnconfirmed(st.Live), linkedGroups(am.groups.Items, gid), gid) {
		return
	}
	probe, err := parseProbe(st.Applied[ProbeSection], s.d.GOARCH)
	if err != nil {
		probe = defaultProbe(s.d.GOARCH)
	}
	rp := repickProbe{cfg: probe}
	if am.installed.Mihomo && s.d.Mihomo != nil && s.d.Running(configlayer.KernelMihomo) && s.d.Mihomo.HasController() {
		rp.up = true
		if b, err := s.mihomoBuildOf(st, am.installed); err == nil && b != nil {
			rp.names = b.nodeNames(g)
			rp.providers = make(map[string]string, len(rp.names))
			for ref, name := range rp.names {
				if prov := b.probeProvider(ref, name); prov != "" {
					rp.providers[ref] = prov
				}
			}
		}
	}

	picked, answered, method := s.pickAlive(ctx, g, am, rp)
	cur := liveSelection(st.Live)[gid]
	failed := false
	if picked != "" && picked != cur {
		switch s.selectWithRetry(ctx, gid, picked) {
		case repickSet:
		case repickFailed:
			// Окончательный отказ: маркер снимается, иначе отвергнутая замена
			// повторялась бы после каждого успешного запуска без предела.
			failed = true
		default:
			return
		}
	}
	s.clearRepick(gid)
	node := picked
	switch {
	case strings.HasPrefix(picked, memberNode):
		node = am.byRef[strings.TrimPrefix(picked, memberNode)].Display
	case strings.HasPrefix(picked, memberGroup):
		if x, ok := am.group(strings.TrimPrefix(picked, memberGroup)); ok {
			node = x.Name
		}
	case picked == memberDirect:
		node = "DIRECT"
	case picked == memberReject:
		node = "REJECT"
	case picked == "":
		node = ""
	}
	ans := "false"
	if answered {
		ans = "true"
	}
	params := map[string]string{"group": g.Name, "node": node, "answered": ans, "method": method}
	if failed {
		params["failed"] = "true"
	}
	s.repick.put(s.d.Layer, gid, configlayer.NoticeView{
		ID: noticeRepickPrefix + gid, Kind: noticeWarning, Params: params,
	})
}

// repickOutcome — исход постановки замены повторным выбором.
type repickOutcome int

const (
	// repickSet — замена поставлена.
	repickSet repickOutcome = iota
	// repickLater — не поставлена из-за занятости или остановки: маркер остаётся
	// для следующего применения, уведомления нет.
	repickLater
	// repickFailed — окончательный отказ (возврат, неизвестный исход, ошибка
	// прохода): маркер снимается, уведомление сообщает об отказе.
	repickFailed
	// repickGone — группа больше не подлежит выбору: маркер снят, уведомления нет.
	repickGone
)

// selectWithRetry ставит выбор синхронным входом (selectSync, без раннего ответа:
// WR-01 ревью 2); при идущем применении повторяет через 30 с, не больше 5 раз.
// Поставленным выбор считается только без ошибки, без возврата (Reverted) и без
// неизвестного исхода (Unconfirmed). Повтор при следующем применении остаётся
// только для занятости и остановки (repickLater): отвергнутая замена,
// оставленная в маркере, повторялась бы после каждого успешного запуска, в том
// числе после выбора пользователя (WR-04 ревью 4).
func (s *Service) selectWithRetry(ctx context.Context, gid, member string) repickOutcome {
	for attempt := 0; ; attempt++ {
		res, err := s.selectSync(ctx, gid, member)
		switch {
		case err == nil && !res.Reverted && len(res.Unconfirmed) == 0:
			return repickSet
		case err == nil && (ctx.Err() != nil || s.ctx.Err() != nil):
			// Ядро остановлено посреди прохода: возврат или неизвестный исход
			// вызваны остановкой, а не отказом. Маркер остаётся, уведомление
			// в памяти при выходе всё равно пропало бы.
			return repickLater
		case err == nil:
			// Возврат без ошибки прохода или неизвестный исход: выбор не поставлен.
			return repickFailed
		case errors.Is(err, ErrGroupNotApplied), errors.Is(err, ErrMemberInvalid):
			s.clearRepick(gid)
			return repickGone
		case errors.Is(err, ErrApplyBusy) && attempt < repickBusyRetries:
			if !s.sleep(ctx, repickBusyDelay) {
				return repickLater
			}
		case errors.Is(err, ErrApplyBusy), ctx.Err() != nil, s.ctx.Err() != nil:
			return repickLater
		default:
			return repickFailed
		}
	}
}

// sleep ждёт d; false — ожидание прервано остановкой.
func (s *Service) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-s.ctx.Done():
		return false
	}
}

// repickProbe — как проверять узлы группы.
type repickProbe struct {
	cfg ProbeConfig
	// up — Mihomo запущен и отвечает: узлы проверяет его delay API.
	up bool
	// names — имена узлов в профиле Mihomo по ссылке.
	names map[string]string
	// providers — провайдер узла по ссылке, если имя из names — имя в файле
	// провайдера (probeProvider): такой узел меряется через свой провайдер.
	providers map[string]string
}

// method — способ проверки по умолчанию.
func (p repickProbe) method() string {
	if p.up {
		return repickMethodDelay
	}
	return repickMethodTLS
}

// probeNode проверяет один узел: Mihomo запущен — задержка через его API
// (ограничение параллелизма держит вызывающий), иначе — TLSPing, «доступность
// сервера по TLS».
func (s *Service) probeNode(ctx context.Context, n sources.CatalogNode, p repickProbe) (alive bool, method string) {
	timeout := time.Duration(p.cfg.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if name := p.names[n.Ref]; p.up && name != "" {
		cctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
		defer cancel()
		var err error
		if prov := p.providers[n.Ref]; prov != "" {
			_, err = s.d.Mihomo.ProviderProxyDelay(cctx, prov, name, p.cfg.URL, timeout)
		} else {
			_, err = s.d.Mihomo.ProxyDelay(cctx, name, p.cfg.URL, timeout)
		}
		return err == nil, repickMethodDelay
	}
	if s.d.Ping == nil {
		return false, repickMethodTLS
	}
	cctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	return s.d.Ping(cctx, n.Node) == nil, repickMethodTLS
}

// pickAlive выбирает первого живого участника по порядку: DIRECT и REJECT только
// если стоят первыми (D-36), вложенная группа — если не пуста, узел — если
// ответил на проверку. Общий раннер проверяет узлы одной пачкой; прежний путь
// использует лимиты параллелизма proxyping.
// Никто не ответил — остаётся первый по порядку (answered=false); method —
// способ проверки ответившего узла, иначе способ по умолчанию.
func (s *Service) pickAlive(ctx context.Context, g Group, am appliedModel, p repickProbe) (picked string, answered bool, method string) {
	cands := am.candidates(g)
	method = p.method()
	first := ""
	for _, c := range cands {
		if (c == memberDirect || c == memberReject) && c != cands[0] {
			continue
		}
		if am.avail(c) {
			first = c
			break
		}
	}
	if s.d.Probe != nil {
		req := proxyping.Request{
			Scope: proxyping.ScopeRepick, Group: g.ID, Method: proxyping.MethodXray,
			URL: p.cfg.URL, Timeout: time.Duration(p.cfg.TimeoutS) * time.Second,
		}
		if req.Timeout <= 0 {
			req.Timeout = 5 * time.Second
		}
		if s.d.KernelState != nil {
			req.Kernel = s.d.KernelState().Kernel
		}
		if p.up {
			req.Method = proxyping.MethodMihomo
		}
		xray := runtimeRefs(am.cat.Xray)
		for _, c := range cands {
			if !strings.HasPrefix(c, memberNode) || !am.avail(c) {
				continue
			}
			n := am.byRef[strings.TrimPrefix(c, memberNode)]
			req.Targets = append(req.Targets, proxyping.Target{
				Ref: n.Ref, Node: n.Node, MihomoName: p.names[n.Ref], MihomoProvider: p.providers[n.Ref],
				Xray: xray[n.Ref], Fingerprint: proxyping.Fingerprint(n.Node),
			})
			if len(req.Targets) == repickMaxProbe {
				break
			}
		}
		if results := s.d.Probe(ctx, req); results != nil {
			method = repickMethodTunnel
			if p.up {
				method = repickMethodDelay
			}
			for i, c := range cands {
				if ctx.Err() != nil {
					break
				}
				if (c == memberDirect || c == memberReject) && i != 0 {
					continue
				}
				if !am.avail(c) {
					continue
				}
				if !strings.HasPrefix(c, memberNode) {
					return c, true, method
				}
				r, ok := results[strings.TrimPrefix(c, memberNode)]
				if !ok {
					continue
				}
				switch r.Method {
				case proxyping.MethodTLS:
					method = repickMethodTLS
				case proxyping.MethodMihomo:
					method = repickMethodDelay
				case proxyping.MethodXray:
					method = repickMethodTunnel
				}
				if r.Status == proxyping.StatusOK {
					return c, true, method
				}
			}
			return first, false, method
		}
	}
	goarch := s.d.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	parallel := proxyping.DefaultLimits(goarch, runtime.NumCPU()).Parallel

	var batch []string
	probed := 0
	flush := func() (string, string, bool) {
		if len(batch) == 0 {
			return "", "", false
		}
		type result struct {
			alive  bool
			method string
		}
		res := make([]result, len(batch))
		var wg sync.WaitGroup
		for i, m := range batch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a, mm := s.probeNode(ctx, am.byRef[strings.TrimPrefix(m, memberNode)], p)
				res[i] = result{a, mm}
			}()
		}
		wg.Wait()
		defer func() { batch = batch[:0] }()
		for i, r := range res {
			if r.alive {
				return batch[i], r.method, true
			}
		}
		return "", "", false
	}

	for i, c := range cands {
		if ctx.Err() != nil {
			break
		}
		switch {
		case c == memberDirect || c == memberReject:
			if i != 0 {
				continue
			}
			if m, mm, ok := flush(); ok {
				return m, true, mm
			}
			return c, true, method
		case strings.HasPrefix(c, memberGroup):
			if !am.avail(c) {
				continue
			}
			if m, mm, ok := flush(); ok {
				return m, true, mm
			}
			return c, true, method
		case strings.HasPrefix(c, memberNode):
			if !am.avail(c) || probed >= repickMaxProbe {
				continue
			}
			batch = append(batch, c)
			probed++
			if len(batch) >= parallel {
				if m, mm, ok := flush(); ok {
					return m, true, mm
				}
			}
		}
	}
	if m, mm, ok := flush(); ok {
		return m, true, mm
	}
	return first, false, method
}
