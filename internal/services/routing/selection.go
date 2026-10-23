package routing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Выбор участника ручной группы без черновика (D-36…D-39). Выбор хранится в
// State.Live[selection] по ID группы и один на оба ядра; Xray читает его при
// сборке файлов (xcp-groups), Mihomo получает его через Clash API.

// LiveKeyRepick — ключ State.Live со списком ID групп, в которых выбранный узел
// пропал: после применения панель заменяет его первым живым по пингу (D-36).
const LiveKeyRepick = "selection_repick"

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
	// selectBusyWait — сколько Select ждёт замок применения, прежде чем
	// ответить apply_busy.
	selectBusyWait = 2 * time.Second
	// reconcileCallTimeout — предел одного обращения к Clash API при сверке.
	reconcileCallTimeout = 10 * time.Second
	// repickParallel — сколько узлов пингуется одновременно (слабый роутер).
	repickParallel = 4
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
	noticeWarning         = "warning"
)

// Способ проверки в уведомлении routing_repick.
const (
	repickMethodDelay = "delay"
	repickMethodTLS   = "tls"
)

// Ошибки выбора участника (коды group_not_applied, member_invalid, apply_busy).
var (
	// ErrGroupNotApplied — группы нет в применённом состоянии, она не ручная или
	// маршрутизацией не управляет панель.
	ErrGroupNotApplied = errors.New("routing: группа не применена")
	// ErrMemberInvalid — участника нет среди вариантов группы.
	ErrMemberInvalid = errors.New("routing: участник вне состава группы")
	// ErrApplyBusy — идёт применение: выбор Xray дождался бы его конца.
	ErrApplyBusy = configlayer.ErrApplyBusy
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
	Kernels map[string]string `json:"kernels"`
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
}

// appliedModelOf читает группы и каталог узлов применённого состояния.
func (s *Service) appliedModelOf(st configlayer.StateView) (appliedModel, error) {
	cfg, err := parseGroups(st.Applied[GroupsSection])
	if err != nil {
		return appliedModel{}, err
	}
	m := appliedModel{groups: cfg, installed: s.d.Installed(), byRef: map[string]sources.CatalogNode{}}
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
func (s *Service) setSelection(gid, member string) error {
	return s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		sel := liveSelection(live)
		sel[gid] = member
		if err := writeSelection(live, sel); err != nil {
			return err
		}
		if ids := readRepick(live); len(ids) > 0 {
			return writeRepick(live, without(ids, gid))
		}
		return nil
	})
}

// restoreSelection возвращает прежнее значение выбора, если никто не успел его
// сменить.
func (s *Service) restoreSelection(gid, member, prev string, had bool) {
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
		return writeSelection(live, sel)
	})
	if err != nil {
		log.Printf("[routing] выбор участника не возвращён: %v", err)
	}
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
// перезапуск, если ядро запущено. Остановленное ядро не запускается: Xray
// прочитает файл при старте, Mihomo получит выбор сверкой при запуске процесса.
// Ревизия черновика не меняется.
func (s *Service) Select(ctx context.Context, groupID, member string) (SelectResult, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return SelectResult{}, err
	}
	if !configlayer.DesiredRouting(st.Applied).Managed {
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

	prev, had := liveSelection(st.Live)[groupID]
	if err := s.setSelection(groupID, member); err != nil {
		return SelectResult{}, err
	}
	res := SelectResult{Member: member, Kernels: map[string]string{
		configlayer.KernelXray:   selNotInstalled,
		configlayer.KernelMihomo: selNotInstalled,
	}}
	if am.installed.Xray {
		out, err := s.selectXray(ctx)
		if err != nil {
			s.restoreSelection(groupID, member, prev, had)
			return SelectResult{}, err
		}
		res.Kernels[configlayer.KernelXray] = out
	}
	if am.installed.Mihomo {
		res.Kernels[configlayer.KernelMihomo] = s.selectMihomo(ctx, groupID)
	}
	return res, nil
}

// memberAllowed — участник есть среди вариантов группы по применённому состоянию
// (те же варианты, что отдаёт представление): участники группы и узлы её состава
// в каждом установленном ядре. Выбор вне состава effectiveSelection всё равно
// игнорировал бы.
func memberAllowed(g Group, member string, am appliedModel) bool {
	var xray, mihomo []sources.CatalogNode
	if am.installed.Xray {
		xray = Resolve(g, am.cat.Xray, am.groups.Items).Nodes
	}
	if am.installed.Mihomo {
		mihomo = Resolve(g, am.cat.Mihomo, am.groups.Items).Nodes
	}
	members := Resolve(g, am.union, am.groups.Items).Members
	for _, o := range memberOptions(g, members, am.groups.Items, xray, mihomo) {
		if o.Member == member {
			return true
		}
	}
	return false
}

// selectXray запускает конвейер для одного Xray: файл xcp-groups меняется,
// запущенный Xray перезапускается. Идущее применение — ErrApplyBusy.
func (s *Service) selectXray(ctx context.Context) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, selectBusyWait)
	defer cancel()
	view, err := s.d.Layer.RunLive(rctx, configlayer.LiveRun{Trigger: configlayer.TriggerSelect, Kernel: configlayer.KernelXray})
	if err != nil {
		if errors.Is(err, ErrApplyBusy) || (rctx.Err() != nil && ctx.Err() == nil) {
			return "", ErrApplyBusy
		}
		return selFailed, nil
	}
	return s.xrayOutcome(view), nil
}

// xrayOutcome переводит итог запуска конвейера в исход выбора для Xray.
func (s *Service) xrayOutcome(view configlayer.ApplyView) string {
	if view.Result == nil || !view.Result.OK {
		return selFailed
	}
	for _, r := range view.Restart {
		if r.Kernel != configlayer.KernelXray {
			continue
		}
		switch r.Outcome {
		case configlayer.RestartOutcomeRestarted, configlayer.RestartOutcomeHotReloaded, configlayer.RestartOutcomeRestartedReload:
			return selRestarted
		case configlayer.RestartOutcomeDeferred, configlayer.RestartOutcomeUntouchedIdle, configlayer.RestartOutcomeUntouchedClash:
			return selPendingStopped
		case configlayer.RestartOutcomeNoRuntimeEffect:
			return selApplied
		default:
			return selFailed
		}
	}
	if s.d.Running(configlayer.KernelXray) {
		return selApplied
	}
	return selPendingStopped
}

// selectMihomo ставит выбор в запущенном Mihomo. Сбой связи или ответа не
// откатывает выбор: Live уже записан, а повторить его поставит сверка.
func (s *Service) selectMihomo(ctx context.Context, gid string) string {
	if !s.d.Running(configlayer.KernelMihomo) {
		return selPendingStopped
	}
	s.selMu.Lock()
	defer s.selMu.Unlock()
	st, err := s.d.Layer.ReadState()
	if err != nil {
		s.markReconcile()
		return selFailed
	}
	plan, err := s.mihomoPlan(st, s.d.Installed())
	if err != nil {
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
		s.markReconcile()
		return selFailed
	}
	cctx, cancel := context.WithTimeout(ctx, reconcileCallTimeout)
	defer cancel()
	if err := s.d.Mihomo.SelectProxy(cctx, entry.group, entry.want); err != nil {
		s.markReconcile()
		return selFailed
	}
	return selApplied
}

// --- выбор в профиле Mihomo ---

// mihomoSelEntry — ожидаемый выбор ручной группы в профиле Mihomo.
type mihomoSelEntry struct {
	gid, group, want string
}

// mihomoBuildOf готовит расчёт профиля Mihomo по применённому состоянию, ровно
// как генератор (имена групп и копий узлов). nil — Mihomo не установлен,
// маршрутизацией не управляет панель или группы ниже порога версии (D-45).
func (s *Service) mihomoBuildOf(st configlayer.StateView, installed configlayer.InstalledKernels) (*mihomoBuild, error) {
	if !installed.Mihomo || !configlayer.DesiredRouting(st.Applied).Managed || !s.mihomoGroupsAvailable() {
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
		g := mg.g
		if g.Mode != ModeManual {
			continue
		}
		names := b.nodeNames(g)
		pick := effectiveSelection(g, live[g.ID], func(m string) bool { return b.memberName(m, names) != "" },
			nodeRefs(b.res[g.ID].Nodes)...)
		if pick == "" {
			continue
		}
		out = append(out, mihomoSelEntry{gid: g.ID, group: g.Name, want: b.memberName(pick, names)})
	}
	return out, nil
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
	if !snap.Enabled {
		return nil
	}
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
	for _, e := range plan {
		cctx, cancel := context.WithTimeout(ctx, reconcileCallTimeout)
		now, err := s.d.Mihomo.ProxyNow(cctx, e.group)
		if err == nil && now != e.want {
			err = s.d.Mihomo.SelectProxy(cctx, e.group, e.want)
		}
		cancel()
		if err == nil {
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
// живым по пингу. Выбор удалённых групп убирается.
func (s *Service) CommitLive(st *configlayer.State, req configlayer.ApplyRequest, used configlayer.Input) error {
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
	if info.Trigger != configlayer.TriggerSelect {
		if err := s.reconcileMihomo(ctx); errors.Is(err, errReconcileRetry) {
			s.markReconcile()
		}
	}
	s.repickAll(ctx)
}

// repickNotes — уведомления routing_repick по группам: хранятся, пока группа
// существует, потому что SetNotices заменяет набор целиком.
type repickNotes struct {
	mu    sync.Mutex
	notes map[string]configlayer.NoticeView
}

// put сохраняет уведомление группы и заменяет набор в слое. Закрытое
// пользователем уведомление той же группы возвращается новым событием: набор
// сначала обновляется без него.
func (r *repickNotes) put(l *configlayer.Layer, gid string, n configlayer.NoticeView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.notes == nil {
		r.notes = map[string]configlayer.NoticeView{}
	}
	if _, had := r.notes[gid]; had {
		delete(r.notes, gid)
		l.SetNotices(noticeRepickPrefix, r.listLocked())
	}
	r.notes[gid] = n
	l.SetNotices(noticeRepickPrefix, r.listLocked())
}

// prune убирает уведомления групп, которых больше нет.
func (r *repickNotes) prune(l *configlayer.Layer, exists func(gid string) bool) {
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
		l.SetNotices(noticeRepickPrefix, r.listLocked())
	}
}

func (r *repickNotes) listLocked() []configlayer.NoticeView {
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
	probe, err := parseProbe(st.Applied[ProbeSection], s.d.GOARCH)
	if err != nil {
		probe = defaultProbe(s.d.GOARCH)
	}
	rp := repickProbe{cfg: probe}
	if am.installed.Mihomo && s.d.Mihomo != nil && s.d.Running(configlayer.KernelMihomo) && s.d.Mihomo.HasController() {
		rp.up = true
		if b, err := s.mihomoBuildOf(st, am.installed); err == nil && b != nil {
			rp.names = b.nodeNames(g)
		}
	}

	picked, answered, method := s.pickAlive(ctx, g, am, rp)
	cur := liveSelection(st.Live)[gid]
	if picked != "" && picked != cur {
		if !s.selectWithRetry(ctx, gid, picked) {
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
	s.repick.put(s.d.Layer, gid, configlayer.NoticeView{
		ID: noticeRepickPrefix + gid, Kind: noticeWarning,
		Params: map[string]string{"group": g.Name, "node": node, "answered": ans, "method": method},
	})
}

// selectWithRetry ставит выбор; при идущем применении повторяет через 30 с, не
// больше 5 раз. false — выбор не поставлен (маркер остаётся для следующего
// применения) или группа больше не подлежит выбору (маркер снят).
func (s *Service) selectWithRetry(ctx context.Context, gid, member string) bool {
	for attempt := 0; ; attempt++ {
		_, err := s.Select(ctx, gid, member)
		switch {
		case err == nil:
			return true
		case errors.Is(err, ErrGroupNotApplied), errors.Is(err, ErrMemberInvalid):
			s.clearRepick(gid)
			return false
		case errors.Is(err, ErrApplyBusy) && attempt < repickBusyRetries:
			if !s.sleep(ctx, repickBusyDelay) {
				return false
			}
		default:
			return false
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
		_, err := s.d.Mihomo.ProxyDelay(cctx, name, p.cfg.URL, timeout)
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
// ответил на проверку. Узлы проверяются пачками не больше repickParallel.
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
			if len(batch) >= repickParallel {
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
