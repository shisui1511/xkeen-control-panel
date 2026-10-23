package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Взятие и возврат маршрутизации (D-01…D-05, D-43, D-44) — переход состояния
// внутри обычного «Применить»: желаемое (секция routing черновика) против
// фактического (State.Routing). Операции плана считает computeRoutingOps,
// выполняет writePlan под тем же набором копий и журналом, проверяют ядра на
// итоговой раскладке (validate.go), фиксирует commitState.

// PanelMihomoProfile — профиль Mihomo панели: после взятия config.yaml указывает
// на него.
const PanelMihomoProfile = "profiles/xcp-main.yaml"

// ReasonRoutingTakeover — причина, по которой чужой файл попал в набор копий
// (перенос «чистого» файла routing или пометка файла, созданного возвратом).
const ReasonRoutingTakeover = "routing_takeover"

// Коды проблем планирования маршрутизации.
const (
	// IssueReleaseConflict — при возврате файл с таким именем уже лежит в
	// configs/ (пользователь вернул его сам): перезаписи нет.
	IssueReleaseConflict = "release_conflict"
	// IssueRoutingScanFailed — каталог ядра не прочитался или config.yaml указывает
	// за пределы каталога Mihomo.
	IssueRoutingScanFailed = "routing_scan_failed"
	// IssueRoutingTakeoverFailed — переносимый файл изменился между сканом и записью.
	IssueRoutingTakeoverFailed = "routing_takeover_failed"
)

// Порты Mihomo по умолчанию — как у старого конструктора профиля.
const (
	defaultMihomoRedirPort  = 5000
	defaultMihomoTProxyPort = 5001
)

// Виды прежнего config.yaml Mihomo.
const (
	configKindSymlink = "symlink"
	configKindFile    = "file"
	configKindNone    = "none"
)

// MovedFile — перенесённый чужой файл routing: имя в configs/, хэш и набор
// копий, где лежит прежнее содержимое (<BackupDir>/xray/<Name>).
type MovedFile struct {
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	BackupDir string `json:"backup_dir"`
}

// XrayRoutingState — фактическое владение маршрутизацией Xray.
type XrayRoutingState struct {
	TakenAt time.Time `json:"taken_at"`
	// Moved — «чистые» файлы routing, перенесённые в резервный набор панели.
	Moved []MovedFile `json:"moved"`
	// Left — файлы с routing, оставленные на месте (смешанные и файлы старого слоя).
	Left []string `json:"left"`
}

// MihomoPorts — порты прозрачного прокси и доступ из LAN прежнего активного
// профиля: профиль панели повторяет их, иначе правила iptables XKeen останутся
// на старых портах (D-05).
type MihomoPorts struct {
	Redir       int    `json:"redir"`
	TProxy      int    `json:"tproxy"`
	AllowLAN    *bool  `json:"allow_lan,omitempty"`
	BindAddress string `json:"bind_address,omitempty"`
}

// MihomoRoutingState — фактическое владение маршрутизацией Mihomo.
type MihomoRoutingState struct {
	TakenAt time.Time `json:"taken_at"`
	// PrevKind — чем был config.yaml: symlink на профиль, обычный файл или его не
	// было (none); пусто — config.yaml уже указывал на профиль панели.
	PrevKind string `json:"prev_kind"`
	// PrevProfile — путь прежнего профиля внутри каталога Mihomo (для обычного
	// файла — config.yaml).
	PrevProfile string `json:"prev_profile"`
	// PrevBackup — каталог набора копий, где лежит прежний обычный config.yaml
	// (<PrevBackup>/mihomo/config.yaml); пусто для симлинка.
	PrevBackup string `json:"prev_backup,omitempty"`
	// PrevLink — текст прежнего симлинка (абсолютный или относительный путь):
	// возврат восстанавливает его в прежнем виде.
	PrevLink string      `json:"prev_link,omitempty"`
	Ports    MihomoPorts `json:"ports"`
}

// RoutingState — фактическое владение маршрутизацией по ядрам; меняется только
// применением (State.Routing).
type RoutingState struct {
	Xray   *XrayRoutingState   `json:"xray,omitempty"`
	Mihomo *MihomoRoutingState `json:"mihomo,omitempty"`
}

// clone возвращает глубокую копию.
func (r RoutingState) clone() RoutingState {
	out := RoutingState{}
	if r.Xray != nil {
		x := *r.Xray
		x.Moved = append([]MovedFile{}, r.Xray.Moved...)
		x.Left = append([]string{}, r.Xray.Left...)
		out.Xray = &x
	}
	if r.Mihomo != nil {
		m := *r.Mihomo
		if r.Mihomo.Ports.AllowLAN != nil {
			v := *r.Mihomo.Ports.AllowLAN
			m.Ports.AllowLAN = &v
		}
		out.Mihomo = &m
	}
	return out
}

// backupDirs — наборы копий, на которые ссылается фактическое владение: ротация
// их не удаляет, иначе возврат не из чего было бы делать.
func (r RoutingState) backupDirs() []string {
	var out []string
	if r.Xray != nil {
		for _, m := range r.Xray.Moved {
			out = append(out, m.BackupDir)
		}
	}
	if r.Mihomo != nil && r.Mihomo.PrevBackup != "" {
		out = append(out, r.Mihomo.PrevBackup)
	}
	return out
}

// RoutingFacts — факты о маршрутизации для генераторов (Input.Routing).
type RoutingFacts struct {
	// XrayAPIInbound — в configs/ есть входящий с тегом api (D-43): правило api
	// файл панели пишет первым.
	XrayAPIInbound bool
	// Mihomo — порты и доступ из LAN: взятого профиля или, пока не взято,
	// текущего активного.
	Mihomo      MihomoPorts
	XrayTaken   bool
	MihomoTaken bool
}

// RoutingFile — результат скана одного файла configs/*.json.
type RoutingFile struct {
	Name string
	// HasRouting — на верхнем уровне есть ключ routing.
	HasRouting bool
	// Pure — routing единственный ключ верхнего уровня (кроме Legacy и разбираемых
	// с ошибкой файлов): такой файл переносится целиком.
	Pure bool
	Keys []string
	// Legacy — файл старого слоя: не переносится, он переписывается старым слоем.
	Legacy bool
	// ForceProxy — в правилах routing есть inboundTag с force-proxy (D-44).
	ForceProxy bool
	// Balancer — признак балансировщика XKeen: balancer с тегом balancer или
	// входящий probe (D-44).
	Balancer bool
	// Broken — файл не разобрался: считается смешанным и не трогается.
	Broken bool
}

// RoutingOps — операции маршрутизации одного плана.
type RoutingOps struct {
	// XrayTake — имена «чистых» файлов routing для переноса в резервный набор.
	XrayTake []string
	// XrayRelease — файлы, возвращаемые в configs/ из резервных наборов.
	XrayRelease []MovedFile
	// MihomoPoint — config.yaml перекладывается на профиль панели.
	MihomoPoint bool
	// MihomoRestore — config.yaml возвращается к прежнему значению.
	MihomoRestore *MihomoRoutingState
	// Next — фактическое владение после применения (Moved заполняется при записи).
	Next RoutingState
	// xrayTaking — это применение берёт Xray (Moved в Next заполняется при записи).
	xrayTaking bool
}

// hasXrayDisk — у Xray есть операции на диске.
func (o *RoutingOps) hasXrayDisk() bool {
	return o != nil && (len(o.XrayTake) > 0 || len(o.XrayRelease) > 0)
}

// hasMihomoDisk — у Mihomo есть операции на диске.
func (o *RoutingOps) hasMihomoDisk() bool {
	return o != nil && (o.MihomoPoint || o.MihomoRestore != nil)
}

// hasDisk — операция над ядром kernel меняет файлы на диске.
func (o *RoutingOps) hasDisk(kernel string) bool {
	switch kernel {
	case KernelXray:
		return o.hasXrayDisk()
	case KernelMihomo:
		return o.hasMihomoDisk()
	}
	return false
}

// safeRoutingName — имя файла в configs/ допустимо для переноса и возврата:
// одно имя верхнего уровня, .json, не файл панели.
func safeRoutingName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	if !strings.HasSuffix(name, ".json") || IsPanelFileName(KernelXray, name) {
		return false
	}
	return true
}

// parseXrayJSON разбирает файл configs/ с комментариями; ошибка — файл
// считается смешанным.
func parseXrayJSON(data []byte) (map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(services.StripJSONComments(string(data))), &top); err != nil {
		return nil, err
	}
	if top == nil {
		return nil, errors.New("не объект")
	}
	return top, nil
}

// scanRoutingFile разбирает один файл каталога Xray.
func scanRoutingFile(name string, data []byte, legacy, symlink bool) (rf RoutingFile, apiInbound bool) {
	rf = RoutingFile{Name: name, Legacy: legacy}
	top, err := parseXrayJSON(data)
	if err != nil {
		rf.Broken = true
		rf.HasRouting = bytes.Contains(data, []byte(`"routing"`))
		return rf, false
	}
	for k := range top {
		rf.Keys = append(rf.Keys, k)
	}
	sort.Strings(rf.Keys)
	routingRaw, has := top["routing"]
	rf.HasRouting = has
	// Симлинк не переносится: удаление ссылки унесло бы не файл, а ссылку, а
	// содержимое лежит вне configs/.
	rf.Pure = has && len(top) == 1 && !legacy && !symlink
	if has {
		var routing struct {
			Rules []struct {
				InboundTag json.RawMessage `json:"inboundTag"`
			} `json:"rules"`
			Balancers []struct {
				Tag string `json:"tag"`
			} `json:"balancers"`
		}
		if json.Unmarshal(routingRaw, &routing) == nil {
			for _, r := range routing.Rules {
				if strings.Contains(string(r.InboundTag), "force-proxy") {
					rf.ForceProxy = true
				}
			}
			for _, b := range routing.Balancers {
				if b.Tag == "balancer" {
					rf.Balancer = true
				}
			}
		}
	}
	if inboundsRaw, ok := top["inbounds"]; ok {
		var inbounds []struct {
			Tag string `json:"tag"`
		}
		if json.Unmarshal(inboundsRaw, &inbounds) == nil {
			for _, in := range inbounds {
				switch in.Tag {
				case "api":
					apiInbound = true
				case "probe":
					rf.Balancer = true
				}
			}
		}
	}
	return rf, apiInbound
}

// ScanXrayRouting сканирует верхний уровень каталога конфигураций Xray: *.json
// без подкаталогов, .obsolete и файлов панели. foreignOwned (может быть nil)
// отмечает файлы старого слоя: они не переносятся. apiInbound — у какого-либо
// файла каталога есть входящий с тегом api.
func ScanXrayRouting(root string, foreignOwned func(kernel, rel string) bool) (files []RoutingFile, apiInbound bool, err error) {
	if root == "" {
		return nil, false, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("каталог Xray: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || IsPanelFileName(KernelXray, name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // оборванный симлинк
			}
			return nil, false, fmt.Errorf("%s: %w", name, err)
		}
		legacy := foreignOwned != nil && foreignOwned(KernelXray, name)
		rf, api := scanRoutingFile(name, data, legacy, e.Type()&fs.ModeSymlink != 0)
		if api {
			apiInbound = true
		}
		files = append(files, rf)
	}
	return files, apiInbound, nil
}

// mihomoActive — результат скана config.yaml Mihomo.
type mihomoActive struct {
	Kind    string
	Profile string
	Link    string
	Ports   MihomoPorts
}

// defaultMihomoPorts — порты, если в прежнем профиле их нет.
func defaultMihomoPorts() MihomoPorts {
	return MihomoPorts{Redir: defaultMihomoRedirPort, TProxy: defaultMihomoTProxyPort}
}

// relInsideRoot возвращает путь abs относительно root (в виде со слэшами), если
// он лежит внутри; корень сравнивается и в исходном, и в развёрнутом виде.
func relInsideRoot(root, abs string) (string, bool) {
	bases := []string{filepath.Clean(root)}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		bases = append(bases, filepath.Clean(r))
	}
	for _, base := range bases {
		rel, err := filepath.Rel(base, filepath.Clean(abs))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.ToSlash(rel), true
	}
	return "", false
}

// scanMihomoActive читает config.yaml Mihomo и порты прежнего активного профиля.
func scanMihomoActive(root string) (mihomoActive, error) {
	out := mihomoActive{Ports: defaultMihomoPorts()}
	if root == "" {
		return out, nil
	}
	cfg := filepath.Join(root, "config.yaml")
	fi, err := os.Lstat(cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			out.Kind = configKindNone
			return out, nil
		}
		return out, fmt.Errorf("config.yaml: %w", err)
	}
	profile := cfg
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(cfg)
		if err != nil {
			return out, fmt.Errorf("config.yaml: %w", err)
		}
		target := link
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		rel, ok := relInsideRoot(root, target)
		if !ok {
			return out, fmt.Errorf("config.yaml: %w", ErrSymlinkOutsideRoot)
		}
		out.Kind, out.Profile, out.Link = configKindSymlink, rel, link
		profile = target
		if rel == PanelMihomoProfile {
			// Уже указывает на профиль панели: переносить нечего.
			out.Kind, out.Profile, out.Link = "", "", ""
		}
	} else {
		out.Kind, out.Profile = configKindFile, "config.yaml"
	}
	if data, err := os.ReadFile(profile); err == nil {
		out.Ports = parseMihomoPorts(data)
	}
	return out, nil
}

// ScanMihomoActive возвращает вид и путь прежнего активного профиля Mihomo и
// его порты (D-05): symlink или обычный файл, путь внутри root; порты —
// redir-port/tproxy-port, иначе port первого listeners[] типа redir/tproxy без
// force-proxy в имени, иначе 5000/5001. config.yaml нет — вид none.
func ScanMihomoActive(root string) (prevKind, prevProfile string, ports MihomoPorts, err error) {
	a, err := scanMihomoActive(root)
	return a.Kind, a.Profile, a.Ports, err
}

// Предупреждения предпросмотра взятия (D-44): взятие они не блокируют.
const (
	// WarnSpeedBalancer — у какого-либо файла каталога признак балансировщика
	// XKeen (тег balancer или входящий probe).
	WarnSpeedBalancer = "speed_balancer"
	// WarnForceProxy — у переносимого файла есть правило с inboundTag force-proxy*.
	WarnForceProxy = "force_proxy"
)

// TakeoverXray — предпросмотр взятия маршрутизации Xray.
type TakeoverXray struct {
	// Move — «чистые» файлы routing, которые уйдут в резервные копии панели.
	Move []string `json:"move"`
	// Left — файлы с routing, которые останутся на месте (смешанные, D-43).
	Left     []string `json:"left"`
	Warnings []string `json:"warnings"`
}

// TakeoverMihomo — предпросмотр взятия маршрутизации Mihomo.
type TakeoverMihomo struct {
	// PrevProfile — прежний активный профиль внутри каталога Mihomo.
	PrevProfile string      `json:"prev_profile"`
	Ports       MihomoPorts `json:"ports"`
}

// TakeoverPreview — предпросмотр взятия для диалога (UI-SPEC E10). Ядро, которого
// нет, остаётся nil.
type TakeoverPreview struct {
	Xray   *TakeoverXray   `json:"xray"`
	Mihomo *TakeoverMihomo `json:"mihomo"`
}

// splitRoutingFiles делит файлы скана на переносимые и оставленные на месте:
// переносится «чистый» файл routing (не старого слоя, с допустимым именем),
// остаются смешанные и не разобравшиеся файлы с ключом routing (D-43). Файл
// старого слоя не переносится и не числится оставленным: он переписывается
// старым слоем. Оба списка отсортированы и не nil.
func splitRoutingFiles(files []RoutingFile) (move, left []string) {
	move, left = []string{}, []string{}
	for _, f := range files {
		switch {
		case f.Pure && !f.Legacy && safeRoutingName(f.Name):
			move = append(move, f.Name)
		case f.HasRouting && !f.Legacy:
			left = append(left, f.Name)
		}
	}
	sort.Strings(move)
	sort.Strings(left)
	return move, left
}

// RoutingPreviewOf строит предпросмотр взятия по результатам сканов: files —
// ScanXrayRouting, prevProfile и ports — ScanMihomoActive. Функция чистая: на
// диск не пишет и ничего не скрывает — оставленные смешанные файлы попадают в
// Left. Предупреждения (D-44) взятие не блокируют.
func RoutingPreviewOf(files []RoutingFile, prevProfile string, ports MihomoPorts) TakeoverPreview {
	move, left := splitRoutingFiles(files)
	warnings := []string{}
	for _, f := range files {
		if f.Balancer {
			warnings = append(warnings, WarnSpeedBalancer)
			break
		}
	}
	moving := make(map[string]bool, len(move))
	for _, n := range move {
		moving[n] = true
	}
	for _, f := range files {
		if f.ForceProxy && moving[f.Name] {
			warnings = append(warnings, WarnForceProxy)
			break
		}
	}
	return TakeoverPreview{
		Xray:   &TakeoverXray{Move: move, Left: left, Warnings: warnings},
		Mihomo: &TakeoverMihomo{PrevProfile: prevProfile, Ports: ports},
	}
}

// yamlInt читает скалярное целое из значения YAML.
func yamlInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// parseMihomoPorts достаёт порты и доступ из LAN из профиля Mihomo; значения
// читаются как скаляры, нераспознанное игнорируется.
func parseMihomoPorts(data []byte) MihomoPorts {
	p := defaultMihomoPorts()
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return p
	}
	redir, tproxy := yamlInt(doc["redir-port"]), yamlInt(doc["tproxy-port"])
	if listeners, ok := doc["listeners"].([]any); ok {
		for _, l := range listeners {
			m, ok := l.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			typ, _ := m["type"].(string)
			if strings.Contains(name, "force-proxy") {
				continue
			}
			port := yamlInt(m["port"])
			switch typ {
			case "redir":
				if redir == 0 {
					redir = port
				}
			case "tproxy":
				if tproxy == 0 {
					tproxy = port
				}
			}
		}
	}
	if redir > 0 {
		p.Redir = redir
	}
	if tproxy > 0 {
		p.TProxy = tproxy
	}
	if v, ok := doc["allow-lan"].(bool); ok {
		p.AllowLAN = &v
	}
	if s, ok := doc["bind-address"].(string); ok {
		p.BindAddress = s
	}
	return p
}

// routingScans — результаты сканов, нужных для расчёта операций.
type routingScans struct {
	Xray   []RoutingFile
	Mihomo mihomoActive
}

// needScan — какие ядра сканировать для расчёта операций.
func needScan(desired RoutingDesired, actual RoutingState, installed InstalledKernels, scope string) (xray, mihomo bool) {
	xray = kernelInScope(KernelXray, installed, scope) && desired.Managed && actual.Xray == nil
	mihomo = kernelInScope(KernelMihomo, installed, scope) && desired.Managed && actual.Mihomo == nil
	return xray, mihomo
}

// scanForOps сканирует только то, что нужно для взятия: возврат использует
// сохранённое состояние и каталоги не читает.
func scanForOps(roots Roots, desired RoutingDesired, actual RoutingState, installed InstalledKernels, scope string, foreignOwned func(kernel, rel string) bool) (routingScans, error) {
	var s routingScans
	needX, needM := needScan(desired, actual, installed, scope)
	if needX {
		files, _, err := ScanXrayRouting(roots.Xray, foreignOwned)
		if err != nil {
			return s, err
		}
		s.Xray = files
	}
	if needM {
		a, err := scanMihomoActive(roots.Mihomo)
		if err != nil {
			return s, err
		}
		s.Mihomo = a
	}
	return s, nil
}

// computeRoutingOps считает операции маршрутизации: желаемое managed ∧
// ¬фактическое — взятие, обратное — возврат. Ядро вне области запуска (scope) и
// неустановленное не трогается. Пустые операции — nil.
func computeRoutingOps(desired RoutingDesired, actual RoutingState, installed InstalledKernels, scope string, scans routingScans, now time.Time) *RoutingOps {
	ops := &RoutingOps{Next: actual.clone()}
	changed := false
	now = now.UTC()

	if kernelInScope(KernelXray, installed, scope) {
		switch {
		case desired.Managed && actual.Xray == nil:
			changed = true
			st := &XrayRoutingState{TakenAt: now, Moved: []MovedFile{}, Left: []string{}}
			move, left := splitRoutingFiles(scans.Xray)
			if len(move) > 0 {
				ops.XrayTake = move
			}
			st.Left = left
			ops.Next.Xray = st
			ops.xrayTaking = true
		case !desired.Managed && actual.Xray != nil:
			changed = true
			ops.XrayRelease = append([]MovedFile(nil), actual.Xray.Moved...)
			ops.Next.Xray = nil
		}
	}

	if kernelInScope(KernelMihomo, installed, scope) {
		switch {
		case desired.Managed && actual.Mihomo == nil:
			changed = true
			ops.MihomoPoint = scans.Mihomo.Kind != ""
			ops.Next.Mihomo = &MihomoRoutingState{
				TakenAt:     now,
				PrevKind:    scans.Mihomo.Kind,
				PrevProfile: scans.Mihomo.Profile,
				PrevLink:    scans.Mihomo.Link,
				Ports:       scans.Mihomo.Ports,
			}
		case !desired.Managed && actual.Mihomo != nil:
			changed = true
			prev := *actual.Mihomo
			ops.MihomoRestore = &prev
			if prev.PrevKind == "" {
				ops.MihomoRestore = nil // config.yaml уже указывал на профиль панели: оставляем
			}
			ops.Next.Mihomo = nil
		}
	}
	if !changed {
		return nil
	}
	return ops
}

// releaseConflicts возвращает имена возвращаемых файлов, которые уже лежат в
// configs/ (T-146-19): перезаписи нет.
func releaseConflicts(roots Roots, ops *RoutingOps) []BuildIssue {
	if ops == nil {
		return nil
	}
	var out []BuildIssue
	for _, mf := range ops.XrayRelease {
		abs, err := roots.Abs(KernelXray, mf.Name)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(abs); err == nil {
			out = append(out, BuildIssue{
				Key:    ManifestKey(KernelXray, mf.Name),
				Code:   IssueReleaseConflict,
				Detail: "файл уже лежит в каталоге конфигураций Xray",
				Params: map[string]string{"file": mf.Name},
			})
		}
	}
	return out
}

// planRouting считает операции маршрутизации для запуска. Запуск, ограниченный
// ключами («Пересобрать»), и фоновые обновления данных состояние владения не
// меняют: взятие и возврат идут при «Применить», установке ядра и включении слоя.
func (p *Pipeline) planRouting(src Sections, actual RoutingState, installed InstalledKernels, req ApplyRequest) (*RoutingOps, error) {
	if len(req.Only) > 0 {
		return nil, nil
	}
	if req.Source != SourceDraft && req.Trigger != TriggerKernelInstalled && req.Trigger != TriggerFlagOn {
		return nil, nil
	}
	desired := DesiredRouting(src)
	scans, err := scanForOps(p.d.Roots, desired, actual, installed, req.Kernel, p.d.ForeignOwned)
	if err != nil {
		return nil, &BuildError{Issues: []BuildIssue{{Key: RoutingSection, Code: IssueRoutingScanFailed, Detail: err.Error()}}}
	}
	ops := computeRoutingOps(desired, actual, installed, req.Kernel, scans, p.d.Now())
	if issues := releaseConflicts(p.d.Roots, ops); len(issues) > 0 {
		return nil, &BuildError{Issues: issues}
	}
	return ops, nil
}

// --- Факты для генераторов -------------------------------------------------

// scanMemo — мемо результатов скана по (имя, размер, mtime) файлов: evaluate
// идёт раз в минуту и по событиям, каждый раз разбирать все файлы незачем.
var scanMemo struct {
	mu     sync.Mutex
	xray   map[string]memoBool
	mihomo map[string]memoPorts
}

type memoBool struct {
	sig string
	val bool
}

type memoPorts struct {
	sig string
	val MihomoPorts
}

// fileSig — подпись файла для мемо: размер и mtime (по цели симлинка).
func fileSig(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d", fi.Size(), fi.ModTime().UnixNano())
}

// xrayDirSig — подпись *.json верхнего уровня каталога Xray (без файлов панели).
func xrayDirSig(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "-"
	}
	var b strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || IsPanelFileName(KernelXray, name) {
			continue
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(fileSig(filepath.Join(root, name)))
		b.WriteByte(';')
	}
	return b.String()
}

// xrayAPIInbound сообщает, есть ли в каталоге входящий с тегом api (с мемо).
func xrayAPIInbound(root string) bool {
	if root == "" {
		return false
	}
	sig := xrayDirSig(root)
	scanMemo.mu.Lock()
	if m, ok := scanMemo.xray[root]; ok && m.sig == sig {
		scanMemo.mu.Unlock()
		return m.val
	}
	scanMemo.mu.Unlock()
	_, api, err := ScanXrayRouting(root, nil)
	if err != nil {
		return false
	}
	scanMemo.mu.Lock()
	if scanMemo.xray == nil {
		scanMemo.xray = map[string]memoBool{}
	}
	scanMemo.xray[root] = memoBool{sig: sig, val: api}
	scanMemo.mu.Unlock()
	return api
}

// mihomoActivePorts возвращает порты текущего активного профиля (с мемо).
func mihomoActivePorts(root string) MihomoPorts {
	if root == "" {
		return defaultMihomoPorts()
	}
	cfg := filepath.Join(root, "config.yaml")
	link, _ := os.Readlink(cfg)
	sig := link + "|" + fileSig(cfg)
	scanMemo.mu.Lock()
	if m, ok := scanMemo.mihomo[root]; ok && m.sig == sig {
		scanMemo.mu.Unlock()
		return clonePorts(m.val)
	}
	scanMemo.mu.Unlock()
	a, err := scanMihomoActive(root)
	if err != nil {
		return defaultMihomoPorts()
	}
	scanMemo.mu.Lock()
	if scanMemo.mihomo == nil {
		scanMemo.mihomo = map[string]memoPorts{}
	}
	scanMemo.mihomo[root] = memoPorts{sig: sig, val: a.Ports}
	scanMemo.mu.Unlock()
	return clonePorts(a.Ports)
}

func clonePorts(p MihomoPorts) MihomoPorts {
	if p.AllowLAN != nil {
		v := *p.AllowLAN
		p.AllowLAN = &v
	}
	return p
}

// routingFacts собирает факты для генераторов; вызывается при каждом
// построении Input. Порты берутся у взятого профиля, пока не взято — у текущего
// активного.
func routingFacts(roots Roots, r RoutingState) RoutingFacts {
	f := RoutingFacts{
		XrayAPIInbound: xrayAPIInbound(roots.Xray),
		XrayTaken:      r.Xray != nil,
		MihomoTaken:    r.Mihomo != nil,
	}
	if r.Mihomo != nil {
		f.Mihomo = clonePorts(r.Mihomo.Ports)
	} else {
		f.Mihomo = mihomoActivePorts(roots.Mihomo)
	}
	return f
}

// --- Выполнение операций ---------------------------------------------------

// ConfigWas — состояние config.yaml Mihomo до перекладки (для отката).
type ConfigWas struct {
	Kind string `json:"kind"`
	// Link — текст симлинка (Kind symlink).
	Link string `json:"link,omitempty"`
}

// JournalRouting — операции маршрутизации незавершённой записи (Journal.Routing).
// Файлы Xray вернёт набор копий (Meta.Files), config.yaml — ConfigWas.
type JournalRouting struct {
	XrayTaken []string `json:"xray_taken,omitempty"`
	// XrayReleased — файлы, возвращаемые в configs/ (имя и хэш): откат удаляет
	// такой файл, только если его содержимое осталось тем, что записала панель.
	XrayReleased []MovedFile `json:"xray_released,omitempty"`
	Config       *ConfigWas  `json:"config,omitempty"`
}

// clone возвращает копию записи журнала.
func (j *JournalRouting) clone() *JournalRouting {
	if j == nil {
		return nil
	}
	out := *j
	out.XrayTaken = append([]string(nil), j.XrayTaken...)
	out.XrayReleased = append([]MovedFile(nil), j.XrayReleased...)
	if j.Config != nil {
		c := *j.Config
		out.Config = &c
	}
	return &out
}

// panelProfileLink — текст симлинка на профиль панели: абсолютный путь, как у
// MihomoProfileService.pointConfigTo.
func panelProfileLink(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Join(abs, filepath.FromSlash(PanelMihomoProfile))
}

// setConfigSymlink атомарно перекладывает config.yaml на link: временный
// симлинк рядом и os.Rename (приём pointConfigTo).
func setConfigSymlink(root, link string) error {
	cfg := filepath.Join(root, "config.yaml")
	tmp := cfg + ".xcp-link"
	_ = os.Remove(tmp)
	if err := os.Symlink(link, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, cfg); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// setConfigFile атомарно заменяет config.yaml обычным файлом, не следуя
// симлинку: временный файл рядом и os.Rename.
func setConfigFile(root string, data []byte) error {
	cfg := filepath.Join(root, "config.yaml")
	tmp, err := os.CreateTemp(root, "atomic-config-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, cfg); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// captureConfig запоминает config.yaml до перекладки: симлинк — текст ссылки,
// обычный файл — копия в наборе, нет файла — none.
func captureConfig(roots Roots, set *BackupSet) (*ConfigWas, error) {
	cfg := filepath.Join(roots.Mihomo, "config.yaml")
	fi, err := os.Lstat(cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &ConfigWas{Kind: configKindNone}, nil
		}
		return nil, fmt.Errorf("config.yaml: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(cfg)
		if err != nil {
			return nil, fmt.Errorf("config.yaml: %w", err)
		}
		return &ConfigWas{Kind: configKindSymlink, Link: link}, nil
	}
	if _, err := set.Save(roots, ManifestKey(KernelMihomo, "config.yaml"), KernelMihomo, "config.yaml", ReasonRoutingTakeover); err != nil {
		return nil, err
	}
	return &ConfigWas{Kind: configKindFile}, nil
}

// undoConfig возвращает config.yaml к состоянию до перекладки. Идемпотентна:
// симлинк на прежнюю цель не переписывается.
func undoConfig(roots Roots, set *BackupSet, w *ConfigWas) error {
	if w == nil || roots.Mihomo == "" {
		return nil
	}
	cfg := filepath.Join(roots.Mihomo, "config.yaml")
	switch w.Kind {
	case configKindSymlink:
		if cur, err := os.Readlink(cfg); err == nil && cur == w.Link {
			return nil
		}
		return setConfigSymlink(roots.Mihomo, w.Link)
	case configKindFile:
		if set == nil {
			return errors.New("config.yaml: набор копий не доступен")
		}
		data, err := os.ReadFile(set.copyPath(KernelMihomo, "config.yaml"))
		if err != nil {
			return fmt.Errorf("config.yaml: копия: %w", err)
		}
		return setConfigFile(roots.Mihomo, data)
	case configKindNone:
		if fi, err := os.Lstat(cfg); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return os.Remove(cfg)
		}
	}
	return nil
}

// undoRoutingJournal возвращает то, что набор копий вернуть не может: config.yaml
// и файлы, возвращённые в configs/. Перенесённые файлы Xray лежат в наборе и
// возвращаются Restore.
func undoRoutingJournal(roots Roots, set *BackupSet, j *JournalRouting) error {
	if j == nil {
		return nil
	}
	var errs []error
	for _, mf := range j.XrayReleased {
		if !safeRoutingName(mf.Name) {
			continue
		}
		abs, err := roots.Abs(KernelXray, mf.Name)
		if err != nil {
			continue
		}
		// Удаляется только записанное панелью: чужое содержимое под тем же именем не трогаем.
		if hashOf(abs) != mf.Hash {
			continue
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("%s: %w", mf.Name, err))
		}
	}
	if err := undoConfig(roots, set, j.Config); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// hashOf возвращает хэш содержимого файла или пустую строку, если файла нет.
func hashOf(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return HashContent(data)
}

// verifyPureFile повторно проверяет перед переносом, что файл всё ещё «чистый»
// (T-146-17): между сканом и записью пользователь мог добавить в него ключи.
func verifyPureFile(abs string) error {
	fi, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("файл — симлинк")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	top, err := parseXrayJSON(data)
	if err != nil {
		return err
	}
	if _, ok := top["routing"]; !ok || len(top) != 1 {
		return errors.New("в файле появились другие ключи")
	}
	return nil
}

// prepareRouting готовит операции маршрутизации до записи журнала: копии
// переносимых файлов и config.yaml уходят в набор, перечень — в журнал. На диске
// ничего не меняется. Сбой возвращается ошибкой записи (набор удаляет вызывающий).
func (p *Pipeline) prepareRouting(set *BackupSet, ops *RoutingOps) (*JournalRouting, []JournalFile, error) {
	if ops == nil {
		return nil, nil, nil
	}
	jr := &JournalRouting{}
	var files []JournalFile
	moved := make([]MovedFile, 0, len(ops.XrayTake))
	for _, name := range ops.XrayTake {
		if !safeRoutingName(name) {
			return nil, nil, fmt.Errorf("%s: %w", name, ErrInvalidPanelPath)
		}
		abs, err := p.d.Roots.Abs(KernelXray, name)
		if err != nil {
			return nil, nil, err
		}
		if err := verifyPureFile(abs); err != nil {
			return nil, nil, fmt.Errorf("%s: %s: %w", IssueRoutingTakeoverFailed, name, err)
		}
		key := ManifestKey(KernelXray, name)
		m, err := set.Save(p.d.Roots, key, KernelXray, name, ReasonRoutingTakeover)
		if err != nil {
			return nil, nil, err
		}
		if !m.Existed {
			return nil, nil, fmt.Errorf("%s: %s: файл исчез", IssueRoutingTakeoverFailed, name)
		}
		moved = append(moved, MovedFile{Name: name, Hash: m.OldHash, BackupDir: set.Dir})
		jr.XrayTaken = append(jr.XrayTaken, name)
		files = append(files, JournalFile{Key: key, Existed: true})
	}
	if ops.xrayTaking && ops.Next.Xray != nil {
		ops.Next.Xray.Moved = moved
	}
	for _, mf := range ops.XrayRelease {
		if !safeRoutingName(mf.Name) {
			return nil, nil, fmt.Errorf("%s: %w", mf.Name, ErrInvalidPanelPath)
		}
		abs, err := p.d.Roots.Abs(KernelXray, mf.Name)
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Lstat(abs); err == nil {
			return nil, nil, releaseConflictError(mf.Name)
		}
		if _, err := os.Stat(movedCopyPath(mf)); err != nil {
			return nil, nil, fmt.Errorf("%s: копия: %w", mf.Name, err)
		}
		// В набор возвращаемый файл не попадает: откат по набору удалил бы файл,
		// который пользователь успел вернуть сам (T-146-19); его отменяет журнал.
		jr.XrayReleased = append(jr.XrayReleased, mf)
	}
	if ops.hasMihomoDisk() {
		was, err := captureConfig(p.d.Roots, set)
		if err != nil {
			return nil, nil, err
		}
		jr.Config = was
		if was.Kind == configKindFile {
			files = append(files, JournalFile{Key: ManifestKey(KernelMihomo, "config.yaml"), Existed: true})
		}
		if ops.MihomoPoint && ops.Next.Mihomo != nil {
			// Вид прежнего config.yaml берётся из набора, а не из скана: файл мог
			// измениться между ними.
			ops.Next.Mihomo.PrevKind, ops.Next.Mihomo.PrevLink = was.Kind, was.Link
			if was.Kind == configKindFile {
				ops.Next.Mihomo.PrevProfile, ops.Next.Mihomo.PrevBackup = "config.yaml", set.Dir
			}
		}
	}
	return jr, files, nil
}

// movedCopyPath — путь копии перенесённого файла в наборе.
func movedCopyPath(mf MovedFile) string {
	return filepath.Join(mf.BackupDir, KernelXray, mf.Name)
}

// ErrReleaseConflict — при возврате файл с таким именем уже лежит в configs/.
var ErrReleaseConflict = errors.New(IssueReleaseConflict)

func releaseConflictError(name string) error {
	return fmt.Errorf("%w: %s", ErrReleaseConflict, name)
}

// applyRoutingOps выполняет операции маршрутизации на диске: перенос файлов в
// резервный набор (удаление из configs/ — копия уже в наборе), возврат файлов и
// перекладка config.yaml. Вызывается после записи файлов панели и до удалений.
func (p *Pipeline) applyRoutingOps(ops *RoutingOps) error {
	if ops == nil {
		return nil
	}
	for _, name := range ops.XrayTake {
		abs, err := p.d.Roots.Abs(KernelXray, name)
		if err != nil {
			return err
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, mf := range ops.XrayRelease {
		abs, err := p.d.Roots.Abs(KernelXray, mf.Name)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(abs); err == nil {
			return releaseConflictError(mf.Name)
		}
		data, err := os.ReadFile(movedCopyPath(mf))
		if err != nil {
			return fmt.Errorf("%s: копия: %w", mf.Name, err)
		}
		if HashContent(data) != mf.Hash {
			return fmt.Errorf("%s: копия не совпадает с сохранённым хэшем", mf.Name)
		}
		if err := p.d.WriteFile(abs, data); err != nil {
			return fmt.Errorf("%s: %w", mf.Name, err)
		}
	}
	root := p.d.Roots.Mihomo
	switch {
	case ops.MihomoPoint:
		if err := setConfigSymlink(root, panelProfileLink(root)); err != nil {
			return fmt.Errorf("config.yaml: %w", err)
		}
	case ops.MihomoRestore != nil:
		r := ops.MihomoRestore
		switch r.PrevKind {
		case configKindSymlink:
			link := r.PrevLink
			if link == "" {
				link = filepath.Join(root, filepath.FromSlash(r.PrevProfile))
			}
			if err := setConfigSymlink(root, link); err != nil {
				return fmt.Errorf("config.yaml: %w", err)
			}
		case configKindFile:
			data, err := os.ReadFile(filepath.Join(r.PrevBackup, KernelMihomo, "config.yaml"))
			if err != nil {
				return fmt.Errorf("config.yaml: копия: %w", err)
			}
			if err := setConfigFile(root, data); err != nil {
				return fmt.Errorf("config.yaml: %w", err)
			}
		case configKindNone:
			if err := os.Remove(filepath.Join(root, "config.yaml")); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("config.yaml: %w", err)
			}
		}
	}
	return nil
}

// overlayXrayRouting накладывает на зеркало Xray итоговую раскладку: переносимые
// файлы убираются, возвращаемые добавляются из резервных наборов.
func overlayXrayRouting(tmp string, ops *RoutingOps) error {
	if ops == nil {
		return nil
	}
	for _, name := range ops.XrayTake {
		if !safeRoutingName(name) {
			return fmt.Errorf("%s: %w", name, ErrInvalidPanelPath)
		}
		if err := os.Remove(filepath.Join(tmp, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, mf := range ops.XrayRelease {
		if !safeRoutingName(mf.Name) {
			return fmt.Errorf("%s: %w", mf.Name, ErrInvalidPanelPath)
		}
		data, err := os.ReadFile(movedCopyPath(mf))
		if err != nil {
			return fmt.Errorf("%s: копия: %w", mf.Name, err)
		}
		if err := os.WriteFile(filepath.Join(tmp, mf.Name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
