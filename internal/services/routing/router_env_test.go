//go:build router

package routing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
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

// rtEnv — слой и сервисы на каталоге теста.
type rtEnv struct {
	layer *configlayer.Layer
	src   *sources.Service
	svc   *Service
	dir   string
}

// rtNeedKernels пропускает тест, если на устройстве нет обоих ядер.
func rtNeedKernels(t *testing.T) {
	t.Helper()
	for _, bin := range []string{rtXrayBin, rtMihomoBin} {
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
func rtNewEnv(t *testing.T) *rtEnv {
	t.Helper()
	rtNeedKernels(t)
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
		DataDir:       dir,
		Roots:         roots,
		Enabled:       enabled.Load,
		Binaries:      func() configlayer.Binaries { return configlayer.Binaries{Xray: rtXrayBin, Mihomo: rtMihomoBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)

	installed := func() configlayer.InstalledKernels { return rtBoth }
	src, err := sources.New(sources.Deps{DataDir: dir, Layer: layer, Installed: installed})
	if err != nil {
		t.Fatalf("sources.New: %v", err)
	}
	t.Cleanup(src.Stop)
	svc, err := New(Deps{DataDir: dir, Layer: layer, Sources: src, Installed: installed})
	if err != nil {
		t.Fatalf("routing.New: %v", err)
	}
	t.Cleanup(svc.Stop)

	for _, g := range src.Generators() {
		if err := layer.RegisterGenerator(g); err != nil {
			t.Fatalf("RegisterGenerator %s: %v", g.ID(), err)
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
	layer.Start()
	svc.Start()
	return &rtEnv{layer: layer, src: src, svc: svc, dir: dir}
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

// catalog — каталог узлов текущего черновика.
func (e *rtEnv) catalog(t *testing.T) sources.Catalog {
	t.Helper()
	cat, err := e.src.Catalog(inputOf(e.state(t)), rtBoth)
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
	return e.svc.Checker().Check(inputOf(e.state(t)), rtBoth)
}
