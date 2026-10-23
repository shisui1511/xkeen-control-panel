//go:build router

package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Общая обвязка роутерных проверок модели маршрутизации: настоящий слой
// «Конфигурация» во временном каталоге рабочего каталога теста, настоящие
// бинарники ядер устройства (проверка `xray run -test` и `mihomo -t` идёт
// конвейером слоя), настоящий сервис источников. Вердикт — ответы слоя и сервиса.

const (
	rtXrayBin   = "/opt/sbin/xray"
	rtMihomoBin = "/opt/sbin/mihomo"
)

// rtBoth — оба ядра установлены.
var rtBoth = configlayer.InstalledKernels{Xray: true, Mihomo: true}

// rtXrayOnly — установлен только Xray.
var rtXrayOnly = configlayer.InstalledKernels{Xray: true}

// rtEnv — слой и сервисы на каталоге теста.
type rtEnv struct {
	layer *configlayer.Layer
	src   *sources.Service
	svc   *Service
	dir   string
	// installed — какие ядра установлены в этом окружении (и у слоя, и у сервисов).
	installed configlayer.InstalledKernels
}

// rtOptions — настройки окружения.
type rtOptions struct {
	installed configlayer.InstalledKernels
	deps      []func(*Deps)
	// selection — хуки выбора участника (CommitLive и AfterApply) зарегистрированы
	// в слое, как это делает cmd/xcp/main.go.
	selection bool
}

// rtOption меняет настройки окружения rtNewEnv.
type rtOption func(*rtOptions)

// rtWithDeps подменяет поля Deps (KernelVersion, Lists, Mihomo, Running, Ping)
// до routing.New. Вызовы rtNewEnv без опций работают как раньше. Этим хуком
// пользуются сценарии версий, списков и выбора участника.
func rtWithDeps(f func(*Deps)) rtOption {
	return func(o *rtOptions) { o.deps = append(o.deps, f) }
}

// rtWithSelection регистрирует в слое хуки выбора участника: фиксацию живых данных
// и сверку после применения (сценарии 146-14).
func rtWithSelection() rtOption {
	return func(o *rtOptions) { o.selection = true }
}

// rtWithKernels задаёт установленные ядра окружения (по умолчанию оба).
func rtWithKernels(k configlayer.InstalledKernels) rtOption {
	return func(o *rtOptions) { o.installed = k }
}

// rtNeedKernels пропускает тест, если на устройстве нет обоих ядер.
func rtNeedKernels(t *testing.T) {
	t.Helper()
	rtNeedBins(t, rtXrayBin, rtMihomoBin)
}

// rtNeedBins пропускает тест, если нет хотя бы одного из бинарников ядер.
func rtNeedBins(t *testing.T, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("ядро %s не установлено на устройстве: %v", filepath.Base(bin), err)
		}
	}
}

// rtLink читает ссылку из данных testdata/links по имени файла без расширения.
func rtLink(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(routertest.LinksDir(t), name+".txt"))
	if err != nil {
		t.Fatalf("данные ссылки %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

// rtNewEnv создаёт слой с генераторами источников и проверкой маршрутизации и
// запускает его вместе с сервисами.
func rtNewEnv(t *testing.T, opts ...rtOption) *rtEnv {
	t.Helper()
	o := rtOptions{installed: rtBoth}
	for _, opt := range opts {
		opt(&o)
	}
	var bins []string
	if o.installed.Xray {
		bins = append(bins, rtXrayBin)
	}
	if o.installed.Mihomo {
		bins = append(bins, rtMihomoBin)
	}
	rtNeedBins(t, bins...)
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "rt-routing-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	roots := configlayer.Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, roots.Mihomo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var enabled atomic.Bool
	enabled.Store(true)
	layer, err := configlayer.New(configlayer.Options{
		DataDir: dir,
		Roots:   roots,
		Enabled: enabled.Load,
		Binaries: func() configlayer.Binaries {
			var b configlayer.Binaries
			if o.installed.Xray {
				b.Xray = rtXrayBin
			}
			if o.installed.Mihomo {
				b.Mihomo = rtMihomoBin
			}
			return b
		},
		// geodata (.dat) для проверки правил с ext:<файл>:<тег>.
		XrayEnv:       func(string) []string { return utils.XrayAssetEnv(os.Environ(), "") },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)

	installed := func() configlayer.InstalledKernels { return o.installed }
	src, err := sources.New(sources.Deps{DataDir: dir, Layer: layer, Installed: installed})
	if err != nil {
		t.Fatalf("sources.New: %v", err)
	}
	t.Cleanup(src.Stop)
	deps := Deps{DataDir: dir, Layer: layer, Sources: src, Installed: installed}
	for _, f := range o.deps {
		f(&deps)
	}
	svc, err := New(deps)
	if err != nil {
		t.Fatalf("routing.New: %v", err)
	}
	t.Cleanup(svc.Stop)
	svc.Attach()

	for _, gens := range [][]configlayer.Generator{src.Generators(), svc.Generators()} {
		for _, g := range gens {
			if err := layer.RegisterGenerator(g); err != nil {
				t.Fatalf("RegisterGenerator %s: %v", g.ID(), err)
			}
		}
	}
	if err := layer.AddLiveCommitter(src.CommitLive); err != nil {
		t.Fatalf("AddLiveCommitter: %v", err)
	}
	if err := layer.OnDriftResolved(src.OnDriftResolved); err != nil {
		t.Fatalf("OnDriftResolved: %v", err)
	}
	if err := layer.RegisterChecker(svc.Checker()); err != nil {
		t.Fatalf("RegisterChecker: %v", err)
	}
	if o.selection {
		if err := layer.AddLiveCommitter(svc.CommitLive); err != nil {
			t.Fatalf("AddLiveCommitter (выбор): %v", err)
		}
		if err := layer.AddAfterApply(svc.AfterApply); err != nil {
			t.Fatalf("AddAfterApply: %v", err)
		}
	}
	layer.Start()
	svc.Start()
	return &rtEnv{layer: layer, src: src, svc: svc, dir: dir, installed: o.installed}
}

// state — текущее состояние слоя.
func (e *rtEnv) state(t *testing.T) configlayer.StateView {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// rev — текущая ревизия черновика.
func (e *rtEnv) rev(t *testing.T) int64 {
	t.Helper()
	return e.state(t).DraftRevision
}

// addMyLinks кладёт узлы в «Мои узлы» через предпросмотр и «Добавить» (ровно как
// пользователь): сервис источников зовёт хук групп по умолчанию.
func (e *rtEnv) addMyLinks(t *testing.T, names ...string) {
	t.Helper()
	links := make([]string, 0, len(names))
	for _, n := range names {
		links = append(links, rtLink(t, n))
	}
	e.addMyRaw(t, links...)
}

// addMyRaw добавляет в «Мои узлы» готовые ссылки (текст как у пользователя).
func (e *rtEnv) addMyRaw(t *testing.T, links ...string) {
	t.Helper()
	view, err := e.src.Preview(context.Background(), nodes.Input{Text: strings.Join(links, "\n")})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if view.Token == "" {
		t.Fatalf("предпросмотр без токена")
	}
	if _, err := e.src.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

// groups читает секцию groups черновика.
func (e *rtEnv) groups(t *testing.T) GroupsConfig {
	t.Helper()
	cfg, err := parseGroups(e.state(t).Draft[GroupsSection])
	if err != nil {
		t.Fatalf("секция groups: %v", err)
	}
	return cfg
}

// rules читает секцию rules черновика.
func (e *rtEnv) rules(t *testing.T) RulesConfig {
	t.Helper()
	cfg, err := parseRules(e.state(t).Draft[RulesSection])
	if err != nil {
		t.Fatalf("секция rules: %v", err)
	}
	return cfg
}

// putSection пишет секцию в черновик в обход сервиса (как общий маршрут
// черновика).
func (e *rtEnv) putSection(t *testing.T, name string, raw json.RawMessage) {
	t.Helper()
	if _, err := e.layer.EditDraft(e.rev(t), name, raw); err != nil {
		t.Fatalf("EditDraft %s: %v", name, err)
	}
}

// dropSection удаляет секцию из черновика в обход сервиса.
func (e *rtEnv) dropSection(t *testing.T, name string) {
	t.Helper()
	if _, err := e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{name: nil}, nil); err != nil {
		t.Fatalf("удаление секции %s: %v", name, err)
	}
}

// replaceGroups правит секцию groups черновика в обход сервиса (без проверок
// сервиса: так в черновик попадают циклы и мусор общего маршрута черновика).
func (e *rtEnv) replaceGroups(t *testing.T, fn func(cfg *GroupsConfig)) {
	t.Helper()
	cfg := e.groups(t)
	fn(&cfg)
	raw, err := encodeGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, GroupsSection, raw)
}

// catalog — каталог узлов текущего черновика.
func (e *rtEnv) catalog(t *testing.T) sources.Catalog {
	t.Helper()
	cat, err := e.src.Catalog(inputOf(e.state(t)), e.installed)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	return cat
}

// applyResult запускает «Применить» и ждёт apply_done. Отказ проверки до начала
// применения возвращается ошибкой (и результата нет).
func (e *rtEnv) applyResult(t *testing.T) (*configlayer.ResultView, error) {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApply(true); err != nil {
		return nil, err
	}
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("шина событий закрыта до apply_done")
			}
			if ev.Type == configlayer.EventApplyDone {
				view, ok := ev.Data.(configlayer.ApplyView)
				if !ok {
					t.Fatalf("apply_done с данными %T", ev.Data)
				}
				return view.Result, nil
			}
		case <-deadline:
			t.Fatal("«Применить» не завершилось за 3 минуты")
		}
	}
}

// check вызывает проверку маршрутизации на черновике.
func (e *rtEnv) check(t *testing.T) []configlayer.BuildIssue {
	t.Helper()
	return e.svc.Checker().Check(inputOf(e.state(t)), e.installed)
}

// --- отдельный процесс Mihomo для сценариев выбора участника (146-14) ---

// rtMihomoCore — отдельный процесс Mihomo в каталоге теста: профиль панели из
// окружения с портами и сокетом контроллера в этом каталоге. Ядро стенда и его
// сокет не трогаются. Процесс можно остановить и запустить снова в том же
// каталоге: cache.db (store-selected) остаётся и восстанавливает прежний выбор
// процесса — ровно как при перезапуске или переключении ядра на устройстве.
type rtMihomoCore struct {
	dir  string
	port int
	svc  *services.MihomoService

	mu   sync.Mutex
	proc *rtProc
}

// rtNewMihomoCore готовит каталог и сервис Clash API отдельного процесса.
func rtNewMihomoCore(t *testing.T) *rtMihomoCore {
	t.Helper()
	rtNeedBins(t, rtMihomoBin)
	dir := rtWorkSubdir(t, "rm-")
	return &rtMihomoCore{dir: dir, port: rtFreePort(t), svc: services.NewMihomoService(rtMihomoBin, "", dir)}
}

// rtWithMihomoCore подключает отдельный процесс к Deps окружения: Clash API
// процесса вместо сокета стенда, Running и PID — по живости процесса.
func rtWithMihomoCore(c *rtMihomoCore) rtOption {
	return rtWithDeps(func(d *Deps) {
		d.Mihomo = c.svc
		d.Running = func(kernel string) bool { return kernel == configlayer.KernelMihomo && c.pid() != 0 }
	})
}

// pid возвращает PID живого процесса или 0.
func (c *rtMihomoCore) pid() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc == nil || !c.proc.alive() {
		return 0
	}
	return c.proc.cmd.Process.Pid
}

// pidOf — функция PID для SelectionWatcher: только Mihomo.
func (c *rtMihomoCore) pidOf(kernel string) int {
	if kernel != configlayer.KernelMihomo {
		return 0
	}
	return c.pid()
}

// stop завершает процесс; повторный вызов безопасен.
func (c *rtMihomoCore) stop() {
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

// rtCopyFiles копирует файлы каталога (без вложенных каталогов); нет каталога —
// ничего не делает.
func rtCopyFiles(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		return
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// start собирает config.yaml процесса из профиля панели окружения (порты
// прозрачного прокси и метка сокетов убраны, socks и сокет контроллера — в
// каталоге теста) и запускает процесс. Файлы провайдеров копируются.
func (c *rtMihomoCore) start(t *testing.T, e *rtEnv) {
	t.Helper()
	data, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		t.Fatalf("профиль панели: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("профиль панели не YAML: %v", err)
	}
	for _, k := range []string{"redir-port", "tproxy-port", "routing-mark"} {
		delete(doc, k)
	}
	sock := filepath.Join(c.dir, "c.sock")
	if len(sock) > 100 {
		t.Fatalf("путь сокета контроллера длиннее предела unix-сокета: %d знаков", len(sock))
	}
	_ = os.Remove(sock)
	doc["socks-port"] = c.port
	doc["bind-address"] = "127.0.0.1"
	doc["allow-lan"] = false
	doc["external-controller-unix"] = sock
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.dir, "config.yaml"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	rtCopyFiles(t, e.mihomoPath(proxyProviderDir), filepath.Join(c.dir, proxyProviderDir))
	rtCopyFiles(t, e.mihomoPath(mihomoRuleProviderDir), filepath.Join(c.dir, mihomoRuleProviderDir))
	p := rtStartMihomoCore(t, c.dir, c.port)
	c.mu.Lock()
	c.proc = p
	c.mu.Unlock()
	rtWaitCond(t, 30*time.Second, "Clash API отдельного процесса", c.svc.HasController)
}

// rtWaitCond ждёт, пока условие станет истинным; не дождался — провал теста.
func rtWaitCond(t *testing.T, wait time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s (%s)", what, wait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// rtTLSPing — проверка узла для Deps.Ping: рукопожатие TLS с сервером узла
// (доступность сервера по TLS), как у панели без запущенного Mihomo.
func rtTLSPing(_ context.Context, n nodes.Node) error {
	res, err := services.TLSPing(net.JoinHostPort(n.Host, strconv.Itoa(n.Port)), "", nil, true)
	if err != nil {
		return err
	}
	if !res.OK {
		return errRtNoAnswer
	}
	return nil
}

// errRtNoAnswer — сервер узла не ответил на рукопожатие.
var errRtNoAnswer = errors.New("сервер узла не ответил")
