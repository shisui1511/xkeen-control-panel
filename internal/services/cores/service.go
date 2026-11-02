package cores

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сервис секций ядер (SEC-01, XCFG-01, XCFG-03, MIHO-01; D-01…D-12): вид секций с
// владельцем и действующими значениями, предпросмотр и взятие в черновик, правка
// «форма + Дополнительно», пресет, отпускание, группа force-proxy, разбор сырого
// блока и «Вернуть файл панели». Сервис правит только черновик; диск меняет
// «Применить» (кроме Reclaim, который идёт под журналом слоя). Значения секций и
// secret в журнал панели не пишутся. Фоновых горутин у сервиса нет.

// Коды ошибок сервиса (как в frontend/src/lib/coresErrorCodes.json).
const (
	codeSectionUnknown     = "section_unknown"
	codeKernelNotInstalled = "kernel_not_installed"
	codeMihomoWithProfile  = "mihomo_with_profile"
	codeSectionNotManaged  = "section_not_managed"
	codeGroupNotFound      = "group_not_found"
	codeRawTooLarge        = "raw_too_large"
	codeRawSyntax          = "raw_syntax"
	codeInvalidInput       = "invalid_input"
	codeXKeenTooOld        = "xkeen_too_old"
	codeNothingToReclaim   = "nothing_to_reclaim"
	codeForeignMixed       = "foreign_mixed"
	codeApplyBusy          = "apply_busy"
)

// checkerID — идентификатор проверки секций ядер в реестре слоя.
const checkerID = "cores_check"

// groupsSection — имя секции черновика с группами маршрутизации (пакет routing
// импортирует cores, обратный импорт невозможен).
const groupsSection = "groups"

// Error — ошибка сервиса с кодом и параметрами текста для клиента (имя файла,
// ключ, строка). Секретов и значений секций в параметрах нет.
type Error struct {
	Code   string
	Params map[string]string
}

// Error возвращает код.
func (e *Error) Error() string { return e.Code }

// errCode собирает ошибку; пары «имя, значение» — параметры (пустое значение
// пропускается).
func errCode(code string, params ...string) *Error {
	e := &Error{Code: code}
	for i := 0; i+1 < len(params); i += 2 {
		if params[i+1] == "" {
			continue
		}
		if e.Params == nil {
			e.Params = map[string]string{}
		}
		e.Params[params[i]] = params[i+1]
	}
	return e
}

// Deps — зависимости сервиса. Создаются в cmd/xcp/main.go.
type Deps struct {
	// Layer — слой «Конфигурация»: черновик, скан чужих файлов, владение.
	Layer *configlayer.Layer
	// Installed — какие ядра установлены сейчас (обязательно).
	Installed func() configlayer.InstalledKernels
	// Running сообщает, запущено ли ядро; nil — «нет».
	Running func(kernel string) bool
}

// Service — сервис секций ядер. Создаётся один раз в cmd/xcp/main.go.
type Service struct {
	d Deps
}

// New создаёт сервис. Слой и источник «установленных ядер» обязательны.
func New(d Deps) (*Service, error) {
	if d.Layer == nil {
		return nil, errors.New("cores: не задан слой конфигурации")
	}
	if d.Installed == nil {
		return nil, errors.New("cores: не задан источник установленных ядер")
	}
	if d.Running == nil {
		d.Running = func(string) bool { return false }
	}
	return &Service{d: d}, nil
}

// Generators возвращает генераторы файлов для Layer.Register: cores_xray.
// Профиль Mihomo собирает генератор маршрутизации из секций (MihomoProfileParts).
func (s *Service) Generators() []configlayer.Generator {
	return []configlayer.Generator{XrayGenerator()}
}

// coresChecker — проверка секций обоих ядер (cores_check).
type coresChecker struct{}

func (coresChecker) ID() string { return checkerID }

func (coresChecker) Check(in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.BuildIssue {
	return append(CheckXray(in, installed), CheckMihomo(in, installed)...)
}

// Checker возвращает проверку cores_check для Layer.Register: CheckXray и
// CheckMihomo.
func (s *Service) Checker() configlayer.Checker { return coresChecker{} }

// Start ничего не запускает: фоновых задач у сервиса нет.
func (s *Service) Start() {}

// Stop ничего не ждёт; идемпотентен.
func (s *Service) Stop() {}

// --- типы ответов (json — как frontend/src/lib/coresContract.json) ---

// SectionView — одна секция ядра в виде.
type SectionView struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	// Files — файлы-источники: файлы панели у взятой секции, чужие — у остальных.
	Files []string `json:"files"`
	// Value — действующее значение: у секции панели — черновик, у чужой — слитое
	// ядром.
	Value    json.RawMessage           `json:"value"`
	Form     json.RawMessage           `json:"form"`
	Advanced string                    `json:"advanced"`
	Pinned   []configlayer.PinnedValue `json:"pinned"`
	Dirty    bool                      `json:"dirty"`
	// PanelFile — файл панели секции (у секций под управлением).
	PanelFile string `json:"panel_file,omitempty"`
	// Blocked — причина, по которой секцию нельзя взять (inbounds_mixed,
	// section_shadowed, xkeen_too_old); BlockedFile — файл-причина.
	Blocked     string   `json:"blocked,omitempty"`
	BlockedFile string   `json:"blocked_file,omitempty"`
	Comments    []string `json:"comments,omitempty"`
	Left        []string `json:"left,omitempty"`
	Drift       []string `json:"drift,omitempty"`
}

// ForceProxyView — входящие force-proxy* и группа, в которую они направлены.
type ForceProxyView struct {
	Tags     []string `json:"tags"`
	Group    string   `json:"group"`
	Editable bool     `json:"editable"`
}

// ProfileView — активный профиль Mihomo и его владелец.
type ProfileView struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// KernelView — секции одного ядра.
type KernelView struct {
	Kernel     string          `json:"kernel"`
	Installed  bool            `json:"installed"`
	Running    bool            `json:"running"`
	Sections   []SectionView   `json:"sections"`
	TakeAll    bool            `json:"take_all"`
	Profile    *ProfileView    `json:"profile,omitempty"`
	ForceProxy *ForceProxyView `json:"force_proxy,omitempty"`
}

// SectionPreview — что произойдёт с секцией при взятии.
type SectionPreview struct {
	ID          string   `json:"id"`
	Move        []string `json:"move"`
	Left        []string `json:"left"`
	Comments    []string `json:"comments"`
	Preset      bool     `json:"preset"`
	Blocked     string   `json:"blocked,omitempty"`
	BlockedFile string   `json:"blocked_file,omitempty"`
}

// TakePreview — предпросмотр взятия запрошенных секций.
type TakePreview struct {
	Kernel         string           `json:"kernel"`
	Sections       []SectionPreview `json:"sections"`
	ForceProxyTags []string         `json:"force_proxy_tags,omitempty"`
}

// --- общие помощники ---

// panelMihomoProfile — профиль панели Mihomo.
const panelMihomoProfile = configlayer.PanelMihomoProfile

// Владельцы секций, которых нет в скане слоя.
const (
	ownerPanel   = "panel"
	ownerPending = "pending"
	ownerDrift   = "drift"
	// ownerReleasing — отпускание ждёт «Применить»: секция ещё у панели, в черновике
	// её уже нет.
	ownerReleasing = "releasing"
)

// strs возвращает срез без nil (JSON-массив, а не null).
func strs(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// checkKernel отклоняет неизвестное ядро.
func checkKernel(kernel string) error {
	if kernel != configlayer.KernelXray && kernel != configlayer.KernelMihomo {
		return errCode(codeInvalidInput, "field", "kernel")
	}
	return nil
}

// needXray пропускает только установленный Xray: секции Mihomo этим действием не
// берутся (D-03), на них отвечает mihomo_with_profile.
func (s *Service) needXray(kernel string) error {
	if err := checkKernel(kernel); err != nil {
		return err
	}
	if kernel == configlayer.KernelMihomo {
		return errCode(codeMihomoWithProfile)
	}
	if !s.d.Installed().Xray {
		return errCode(codeKernelNotInstalled)
	}
	return nil
}

// needSection проверяет ядро (установлено) и секцию (белый список, T-147-46).
func (s *Service) needSection(kernel, id string) error {
	if err := checkKernel(kernel); err != nil {
		return err
	}
	if _, ok := kindOf(kernel, id); !ok {
		return errCode(codeSectionUnknown)
	}
	if !s.d.Installed().Has(kernel) {
		return errCode(codeKernelNotInstalled)
	}
	return nil
}

// sectionIDs проверяет список секций Xray по белому списку; пустой список — все
// секции по порядку показа. Повторы отбрасываются.
func sectionIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return slices.Clone(configlayer.XraySectionIDs), nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(configlayer.XraySectionIDs, id) {
			return nil, errCode(codeSectionUnknown)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// edit читает состояние, проверяет ревизию и записывает секции, которые вернул
// fn, одной правкой черновика. Конфликт ревизий — configlayer.ErrDraftConflict без
// записи.
func (s *Service) edit(rev int64, fn func(st configlayer.StateView) (map[string]json.RawMessage, error)) (configlayer.DraftEvent, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	sections, err := fn(st)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.d.Layer.EditDraftBatch(rev, sections, nil)
}

// parseSection разбирает секцию черновика; испорченная — core_section_invalid.
func parseSection(st configlayer.StateView, name string) (configlayer.CoreSection, error) {
	cs, err := configlayer.ParseCoreSection(st.Draft[name])
	if err != nil {
		return configlayer.CoreSection{}, errCode(configlayer.IssueCoreSectionInvalid)
	}
	return cs, nil
}

// encodeSection кодирует секцию для черновика.
func encodeSection(cs configlayer.CoreSection) (json.RawMessage, error) {
	return configlayer.EncodeCoreSection(cs)
}

// sectionDirty — секция черновика отличается от применённой.
func sectionDirty(st configlayer.StateView, name string) bool {
	d, errD := configlayer.ParseCoreSection(st.Draft[name])
	a, errA := configlayer.ParseCoreSection(st.Applied[name])
	if errD != nil || errA != nil {
		return !configlayer.SectionEqual(st.Draft[name], st.Applied[name])
	}
	if !d.Managed && !a.Managed {
		return d.ForceProxyGroup != a.ForceProxyGroup
	}
	de, errD := encodeSection(d)
	ae, errA := encodeSection(a)
	if errD != nil || errA != nil {
		return true
	}
	return !configlayer.SectionEqual(de, ae)
}

// xrayBlock возвращает причину, по которой секцию Xray нельзя взять, и файл-причину.
func xrayBlock(id string, sc configlayer.XraySectionScan, files []configlayer.XrayKeyFile, tooOld bool) (code, file string) {
	if id == "inbounds" && sc.InboundsMixed {
		legacy := map[string]bool{}
		for _, f := range files {
			if f.Legacy {
				legacy[f.Name] = true
			}
		}
		for _, name := range sc.Mixed {
			if !legacy[name] {
				return configlayer.IssueInboundsMixed, name
			}
		}
		return configlayer.IssueInboundsMixed, ""
	}
	if len(sc.Shadowing) > 0 {
		return configlayer.IssueSectionShadowed, sc.Shadowing[0]
	}
	if id == "inbounds" && tooOld {
		return codeXKeenTooOld, ""
	}
	return "", ""
}

// xkeenTooOld — XKeen не умеет управляемые входящие.
func (s *Service) xkeenTooOld() bool {
	return s.d.Layer.FeatureAvailability(configlayer.FeatureXKeenManagedInbounds) == configlayer.Unavailable
}

// forceProxyNames оставляет теги или имена входящих force-proxy*.
func forceProxyNames(names []string) []string {
	var out []string
	for _, n := range names {
		if configlayer.IsForceProxyTag(n) && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// valueIDs возвращает идентификаторы элементов списка значения (тег или имя).
func valueIDs(items json.RawMessage, idKey string) []string {
	list, err := listItems(items)
	if err != nil {
		return nil
	}
	var out []string
	for _, it := range list {
		if id := itemID(it, idKey); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// --- вид ---

// View возвращает секции ядра: владельца, файлы, действующее значение, поля формы,
// «Дополнительно», закреплённые значения, признаки правок и блокировок.
func (s *Service) View(kernel string) (KernelView, error) {
	if err := checkKernel(kernel); err != nil {
		return KernelView{}, err
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return KernelView{}, err
	}
	v := KernelView{
		Kernel:    kernel,
		Installed: s.d.Installed().Has(kernel),
		Running:   s.d.Running(kernel),
		Sections:  []SectionView{},
	}
	if !v.Installed {
		return v, nil
	}
	if kernel == configlayer.KernelXray {
		err = s.viewXray(&v, st)
	} else {
		err = s.viewMihomo(&v, st)
	}
	return v, err
}

// fillForm заполняет форму и «Дополнительно» секции из её значения; значение,
// которое не раскладывается, показывается в «Дополнительно» целиком.
func fillForm(sv *SectionView, kernel string) {
	form, adv, err := SplitForm(kernel, sv.ID, sv.Value)
	if err != nil {
		sv.Form = json.RawMessage("{}")
		sv.Advanced = ""
		if !isEmptyValue(sv.Value) {
			var buf bytes.Buffer
			if json.Indent(&buf, sv.Value, "", "  ") == nil {
				sv.Advanced = buf.String()
			}
		}
		return
	}
	sv.Form, sv.Advanced = form, adv
}

func (s *Service) viewXray(v *KernelView, st configlayer.StateView) error {
	scans, files, err := s.d.Layer.ScanXraySections()
	if err != nil {
		return err
	}
	snap := s.d.Layer.Snapshot()
	desired := configlayer.DesiredCores(st.Draft)
	applied := configlayer.DesiredCores(st.Applied)
	fileDrift := map[string]bool{}
	for _, f := range snap.Files {
		if f.State.IsDrift() {
			fileDrift[f.Key] = true
		}
	}
	tooOld := s.xkeenTooOld()
	byID := map[string]json.RawMessage{}

	for _, spec := range XraySpecs {
		sc := scans[spec.ID]
		cs := desired[spec.Section]
		cn := snap.Cores.Xray[spec.ID]
		taken := cn.Taken
		drift := len(cn.Drift) > 0 || fileDrift[configlayer.ManifestKey(configlayer.KernelXray, spec.File)]

		sv := SectionView{ID: spec.ID, Files: strs(sc.Files), Pinned: []configlayer.PinnedValue{}, Dirty: sectionDirty(st, spec.Section)}
		switch {
		case drift:
			sv.Owner = ownerDrift
		case cs.Managed && taken:
			sv.Owner = ownerPanel
		case cs.Managed:
			sv.Owner = ownerPending
		case taken:
			sv.Owner = ownerReleasing
		case sc.Owner == "":
			sv.Owner = configlayer.OwnerNone
		default:
			sv.Owner = sc.Owner
		}

		switch {
		case cs.Managed:
			sv.Value = cs.Value
			if isEmptyValue(sv.Value) {
				sv.Value = XrayPreset(spec.ID)
			}
		case taken:
			// Отпускание ещё не применено: в силе прежнее значение панели.
			if ac := applied[spec.Section]; ac.Managed && !isEmptyValue(ac.Value) {
				sv.Value = ac.Value
			} else {
				sv.Value = sc.Effective
			}
		default:
			sv.Value = sc.Effective
		}
		if cs.Managed || taken {
			sv.PanelFile = spec.File
			sv.Pinned = append(sv.Pinned, XrayPinned(spec.ID, sv.Value)...)
		}
		if taken {
			sv.Files = []string{spec.File}
			sv.Left = cn.Left
		} else {
			sv.Left = sc.Mixed
			sv.Comments = sc.Comments
			if !cs.Managed {
				sv.Blocked, sv.BlockedFile = xrayBlock(spec.ID, sc, files, tooOld)
			}
		}
		if len(sv.Left) == 0 {
			sv.Left = nil
		}
		if len(sv.Comments) == 0 {
			sv.Comments = nil
		}
		sv.Drift = cn.Drift
		if len(sv.Drift) == 0 {
			sv.Drift = nil
		}
		fillForm(&sv, configlayer.KernelXray)
		byID[spec.ID] = sv.Value
		if !cs.Managed && !taken && sv.Blocked == "" {
			v.TakeAll = true
		}
		v.Sections = append(v.Sections, sv)
	}

	tags := configlayer.XrayFacts(files).ForceProxyTags
	for _, tag := range forceProxyNames(valueIDs(byID["inbounds"], "tag")) {
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) > 0 {
		v.ForceProxy = &ForceProxyView{
			Tags:     tags,
			Group:    desired[configlayer.SectionXrayInbounds].ForceProxyGroup,
			Editable: configlayer.DesiredRouting(st.Draft).ManagedFor(configlayer.KernelXray),
		}
	}
	return nil
}

func (s *Service) viewMihomo(v *KernelView, st configlayer.StateView) error {
	snap := s.d.Layer.Snapshot()
	desired := configlayer.DesiredCores(st.Draft)
	routingManaged := configlayer.DesiredRouting(st.Draft).ManagedFor(configlayer.KernelMihomo)
	taken := snap.Routing.Mihomo != nil && snap.Routing.Mihomo.Taken
	name, data, _, perr := s.d.Layer.MihomoActiveProfile()

	prof := &ProfileView{Name: name}
	switch {
	case taken:
		// Отпускание маршрутизации ещё не применено: профиль пока у панели.
		prof.Owner = ownerPanel
		if !routingManaged {
			prof.Owner = ownerReleasing
		}
		if perr != nil {
			prof.Name = panelMihomoProfile
		}
	case routingManaged:
		prof.Owner = ownerPending
	case perr != nil:
		prof.Owner = configlayer.OwnerNone
	case name == "config.yaml":
		prof.Owner = configlayer.OwnerXKeen
	default:
		prof.Owner = configlayer.OwnerUser
	}
	v.Profile = prof
	panelish := prof.Owner == ownerPanel || prof.Owner == ownerPending
	releasing := prof.Owner == ownerReleasing
	applied := configlayer.DesiredCores(st.Applied)

	var seeds map[string]json.RawMessage
	if !panelish && !releasing && perr == nil {
		// Значения чужого профиля — только для чтения; профиль не разбирается —
		// секции пустые.
		seeds, _ = SeedMihomo(data, configlayer.MihomoPorts{})
	}
	byID := map[string]json.RawMessage{}
	for _, spec := range MihomoSpecs {
		cs := desired[spec.Section]
		sv := SectionView{
			ID:     spec.ID,
			Owner:  prof.Owner,
			Files:  []string{},
			Pinned: []configlayer.PinnedValue{},
			Dirty:  sectionDirty(st, spec.Section),
		}
		if (name != "" && perr == nil) || taken {
			sv.Files = []string{prof.Name}
		}
		switch {
		case releasing:
			// Отпускание не применено: на диске действует профиль панели с прежними
			// значениями секций, их и показываем, только для чтения.
			ac := applied[spec.Section]
			sv.Value = ac.Value
			if !ac.Managed || isEmptyValue(sv.Value) {
				sv.Value = MihomoPreset(spec.ID, configlayer.MihomoPorts{})
			}
			sv.PanelFile = panelMihomoProfile
			sv.Pinned = append(sv.Pinned, MihomoPinned(spec.ID)...)
		case panelish:
			sv.Value = cs.Value
			if !cs.Managed || isEmptyValue(sv.Value) {
				sv.Value = MihomoPreset(spec.ID, configlayer.MihomoPorts{})
			}
			sv.PanelFile = panelMihomoProfile
			sv.Pinned = append(sv.Pinned, MihomoPinned(spec.ID)...)
		default:
			sv.Value = seeds[spec.ID]
			if isEmptyValue(sv.Value) {
				sv.Value = json.RawMessage("{}")
			}
		}
		fillForm(&sv, configlayer.KernelMihomo)
		byID[spec.ID] = sv.Value
		v.Sections = append(v.Sections, sv)
	}

	var names []string
	if obj := objOf(byID[mihomoInbounds]); obj.Len() > 0 {
		if raw, ok := obj.Get("listeners"); ok {
			names = valueIDs(raw, "name")
		}
	}
	if tags := forceProxyNames(names); len(tags) > 0 {
		v.ForceProxy = &ForceProxyView{
			Tags:     tags,
			Group:    desired[configlayer.SectionMihomoInbounds].ForceProxyGroup,
			Editable: routingManaged,
		}
	}
	return nil
}

// --- предпросмотр и взятие ---

// Preview показывает, что произойдёт при взятии запрошенных секций Xray: файлы,
// которые уйдут в копии, остающиеся на месте, файлы с комментариями среди
// переносимых, заполнение из пресета и причина блокировки. Берутся ровно
// запрошенные секции (D-03); пустой список — все. Секции Mihomo —
// mihomo_with_profile.
func (s *Service) Preview(kernel string, ids []string) (TakePreview, error) {
	if err := s.needXray(kernel); err != nil {
		return TakePreview{}, err
	}
	list, err := sectionIDs(ids)
	if err != nil {
		return TakePreview{}, err
	}
	if _, err := s.d.Layer.ReadState(); err != nil {
		return TakePreview{}, err
	}
	scans, files, err := s.d.Layer.ScanXraySections()
	if err != nil {
		return TakePreview{}, err
	}
	tooOld := s.xkeenTooOld()
	out := TakePreview{Kernel: kernel, Sections: []SectionPreview{}}
	for _, id := range list {
		sc := scans[id]
		sp := SectionPreview{
			ID:       id,
			Move:     strs(sc.Pure),
			Left:     strs(sc.Mixed),
			Comments: []string{},
			Preset:   len(sc.Files) == 0,
		}
		for _, name := range sc.Comments {
			if slices.Contains(sc.Pure, name) {
				sp.Comments = append(sp.Comments, name)
			}
		}
		sp.Blocked, sp.BlockedFile = xrayBlock(id, sc, files, tooOld)
		out.Sections = append(out.Sections, sp)
		if id == "inbounds" && len(sc.ForceProxyTags) > 0 {
			out.ForceProxyTags = slices.Clone(sc.ForceProxyTags)
		}
	}
	return out, nil
}

// Take один раз переносит действующее значение запрошенных секций Xray целиком в
// черновик (D-01): значение, файлы-источники с md5 (from), теги входящих, которые
// читает XKeen (keep), и согласие на потерю комментариев (comments_ack). Секция
// без файла заполняется из пресета (D-04). Все секции — одной ревизией (D-03).
// Заблокированная секция: единственная запрошенная — ошибка с кодом блокировки,
// среди нескольких — пропускается. Комментарии в переносимых файлах без ack —
// comments_not_acknowledged. Секция, уже взятая в черновик, не перезаписывается.
func (s *Service) Take(rev int64, kernel string, ids []string, ackComments bool) (configlayer.DraftEvent, error) {
	if err := s.needXray(kernel); err != nil {
		return configlayer.DraftEvent{}, err
	}
	list, err := sectionIDs(ids)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		scans, files, err := s.d.Layer.ScanXraySections()
		if err != nil {
			return nil, err
		}
		snap := s.d.Layer.Snapshot()
		tooOld := s.xkeenTooOld()
		desired := configlayer.DesiredCores(st.Draft)
		applied := configlayer.DesiredCores(st.Applied)
		out := map[string]json.RawMessage{}
		var firstBlock *Error
		for _, id := range list {
			spec, _ := xraySpecByID(id)
			cs := desired[spec.Section]
			if cs.Managed {
				continue
			}
			if snap.Cores.Xray[id].Taken {
				// Отпускание не применено: «Управлять» отменяет его и возвращает
				// прежнее значение панели.
				if ac := applied[spec.Section]; ac.Managed {
					raw, err := encodeSection(ac)
					if err != nil {
						return nil, err
					}
					out[spec.Section] = raw
				}
				continue
			}
			sc := scans[id]
			if code, file := xrayBlock(id, sc, files, tooOld); code != "" {
				e := errCode(code, "file", file)
				if len(list) == 1 {
					return nil, e
				}
				if firstBlock == nil {
					firstBlock = e
				}
				continue
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
			next := configlayer.CoreSection{
				Managed:         true,
				Value:           sc.Effective,
				From:            sc.Sources,
				CommentsAck:     ackComments && len(commentFiles) > 0,
				ForceProxyGroup: cs.ForceProxyGroup,
			}
			if len(sc.Files) == 0 || isEmptyValue(next.Value) {
				next.Value = XrayPreset(id)
				next.Preset = true
			}
			if id == "inbounds" {
				next.Keep = XrayKeepTags(next.Value)
			}
			raw, err := encodeSection(next)
			if err != nil {
				return nil, err
			}
			out[spec.Section] = raw
		}
		if len(out) == 0 && firstBlock != nil {
			return nil, firstBlock
		}
		return out, nil
	})
}

// --- правка ---

// editablePrev возвращает прежнее значение секции, которую можно править. Секция
// Xray должна быть под управлением панели (section_not_managed); секция Mihomo —
// под управлением или при управляемой маршрутизации Mihomo (секции нет — основа из
// встроенной базы).
func editablePrev(kernel, id string, cs configlayer.CoreSection, st configlayer.StateView) (json.RawMessage, error) {
	if kernel == configlayer.KernelXray {
		if !cs.Managed {
			return nil, errCode(codeSectionNotManaged)
		}
		if isEmptyValue(cs.Value) {
			return XrayPreset(id), nil
		}
		return cs.Value, nil
	}
	if !cs.Managed && !configlayer.DesiredRouting(st.Draft).ManagedFor(configlayer.KernelMihomo) {
		return nil, errCode(codeSectionNotManaged)
	}
	if !cs.Managed || isEmptyValue(cs.Value) {
		return MihomoPreset(id, configlayer.MihomoPorts{}), nil
	}
	return cs.Value, nil
}

// keepOf — теги защищённых входящих секции: сохранённые при взятии и найденные в
// прежнем значении.
func keepOf(kernel, id string, cs configlayer.CoreSection, prev json.RawMessage) []string {
	if kernel != configlayer.KernelXray || id != "inbounds" {
		return nil
	}
	keep := slices.Clone(cs.Keep)
	for _, tag := range XrayKeepTags(prev) {
		if !slices.Contains(keep, tag) {
			keep = append(keep, tag)
		}
	}
	return keep
}

// Save сохраняет секцию: поля формы и текст «Дополнительно» собираются в значение
// целиком с порядком ключей прежнего значения; поле, которого форма не знает, не
// пропадает (XCFG-01), очистка поля формы снимает ключ. Проблема ключа — ошибка
// с кодом первой проблемы и ключом в Params["key"] без записи; синтаксическая
// ошибка — raw_syntax со строкой.
func (s *Service) Save(rev int64, kernel, id string, form json.RawMessage, advanced string) (configlayer.DraftEvent, error) {
	if err := s.needSection(kernel, id); err != nil {
		return configlayer.DraftEvent{}, err
	}
	name := configlayer.CoreSectionName(kernel, id)
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		cs, err := parseSection(st, name)
		if err != nil {
			return nil, err
		}
		prev, err := editablePrev(kernel, id, cs, st)
		if err != nil {
			return nil, err
		}
		// Save отклоняет только ключи контроллера сохраняемой секции, secret берётся
		// из её самой и соседних секций черновика, как их увидит «Применить». Снятие
		// secret в другой секции Save пропускает: CheckMihomo покажет
		// controller_no_secret у секции с контроллером в checks снимка и заблокирует
		// «Применить» (409 apply_blocked).
		var siblings []*configlayer.OrderedObject
		if kernel == configlayer.KernelMihomo {
			siblings = mihomoSiblingTops(st.Draft, id)
		}
		value, issues, err := mergeForm(kernel, id, prev, form, advanced, keepOf(kernel, id, cs, prev), siblings)
		if err != nil {
			var re *RawError
			if errors.As(err, &re) {
				return nil, errCode(codeRawSyntax, "line", strconv.Itoa(re.Line), "detail", re.Detail)
			}
			return nil, err
		}
		if len(issues) > 0 {
			return nil, errCode(issues[0].Code, "key", issues[0].Key)
		}
		cs.Managed = true
		cs.Value = value
		raw, err := encodeSection(cs)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{name: raw}, nil
	})
}

// presetValue возвращает пресет секции для замены значения. У «Входящих» Xray
// входящие, которые читает XKeen и которых нет в пресете, остаются.
func presetValue(kernel, id string, prev json.RawMessage, keep []string) json.RawMessage {
	if kernel == configlayer.KernelMihomo {
		return MihomoPreset(id, configlayer.MihomoPorts{})
	}
	preset := XrayPreset(id)
	if id != "inbounds" || len(keep) == 0 {
		return preset
	}
	items, _ := xrayInbounds(preset)
	have := map[string]bool{}
	for _, it := range items {
		have[xrayInboundTag(it)] = true
	}
	old, _ := xrayInbounds(prev)
	for _, it := range old {
		if tag := xrayInboundTag(it); slices.Contains(keep, tag) && !have[tag] {
			items = append(items, it)
		}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return preset
	}
	return b
}

// Preset заменяет значение секции пресетом в черновике (D-04).
func (s *Service) Preset(rev int64, kernel, id string) (configlayer.DraftEvent, error) {
	if err := s.needSection(kernel, id); err != nil {
		return configlayer.DraftEvent{}, err
	}
	name := configlayer.CoreSectionName(kernel, id)
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		cs, err := parseSection(st, name)
		if err != nil {
			return nil, err
		}
		prev, err := editablePrev(kernel, id, cs, st)
		if err != nil {
			return nil, err
		}
		cs.Managed = true
		cs.Value = presetValue(kernel, id, prev, keepOf(kernel, id, cs, prev))
		raw, err := encodeSection(cs)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{name: raw}, nil
	})
}

// Release снимает управление только у этой секции Xray (D-05): файл вернётся при
// «Применить», остальные секции не меняются (D-03). Секции Mihomo отпускаются
// вместе с профилем — mihomo_with_profile.
func (s *Service) Release(rev int64, kernel, id string) (configlayer.DraftEvent, error) {
	if err := s.needXray(kernel); err != nil {
		return configlayer.DraftEvent{}, err
	}
	if _, ok := xraySpecByID(id); !ok {
		return configlayer.DraftEvent{}, errCode(codeSectionUnknown)
	}
	name := configlayer.CoreSectionName(kernel, id)
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		cs, err := parseSection(st, name)
		if err != nil {
			return nil, err
		}
		raw, err := encodeSection(configlayer.CoreSection{Managed: false, ForceProxyGroup: cs.ForceProxyGroup})
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{name: raw}, nil
	})
}

// groupExists сообщает, есть ли группа с таким ID в секции групп черновика.
func groupExists(draft configlayer.Sections, id string) bool {
	var cfg struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if json.Unmarshal(draft[groupsSection], &cfg) != nil {
		return false
	}
	for _, g := range cfg.Items {
		if g.ID == id {
			return true
		}
	}
	return false
}

// SetForceProxyGroup сохраняет группу для входящих force-proxy* в секции
// «Входящие» (D-12). Секция может быть и за XKeen. Пустая группа — значение по
// умолчанию; группы нет в секции групп — group_not_found.
func (s *Service) SetForceProxyGroup(rev int64, kernel, group string) (configlayer.DraftEvent, error) {
	if err := checkKernel(kernel); err != nil {
		return configlayer.DraftEvent{}, err
	}
	if !s.d.Installed().Has(kernel) {
		return configlayer.DraftEvent{}, errCode(codeKernelNotInstalled)
	}
	name := configlayer.CoreSectionName(kernel, "inbounds")
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if group != "" && !groupExists(st.Draft, group) {
			return nil, errCode(codeGroupNotFound)
		}
		cs, err := parseSection(st, name)
		if err != nil {
			return nil, err
		}
		cs.ForceProxyGroup = group
		raw, err := encodeSection(cs)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{name: raw}, nil
	})
}

// ParseRaw проверяет «сырой» блок секции без записи (см. функцию ParseRaw).
func (s *Service) ParseRaw(kernel, id, text string) (RawResult, error) {
	if err := checkKernel(kernel); err != nil {
		return RawResult{}, err
	}
	// Секции черновика дают secret контроллера так же, как у Save. Ошибка чтения —
	// secret только из текста: строже, окончательно решают Save и CheckMihomo.
	var siblings []*configlayer.OrderedObject
	if kernel == configlayer.KernelMihomo {
		if st, err := s.d.Layer.ReadState(); err == nil {
			siblings = mihomoSiblingTops(st.Draft, id)
		}
	}
	return parseRaw(kernel, id, text, siblings)
}

// Reclaim — «Вернуть файл панели»: решает дрейф секции Xray, оставляя секцию у
// панели (Layer.ReclaimSection). Под журналом слоя.
func (s *Service) Reclaim(ctx context.Context, kernel, id string) error {
	if err := s.needXray(kernel); err != nil {
		return err
	}
	if _, ok := xraySpecByID(id); !ok {
		return errCode(codeSectionUnknown)
	}
	err := s.d.Layer.ReclaimSection(ctx, id)
	var mixed *configlayer.ForeignMixedError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, configlayer.ErrNothingToReclaim):
		return errCode(codeNothingToReclaim)
	case errors.As(err, &mixed):
		return errCode(codeForeignMixed, "file", mixed.File)
	case errors.Is(err, configlayer.ErrUnknownSection):
		return errCode(codeSectionUnknown)
	case errors.Is(err, configlayer.ErrApplyBusy), errors.Is(err, configlayer.ErrKernelBusy):
		return errCode(codeApplyBusy)
	}
	return err
}
