//go:build router

package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Общая обвязка роутерных проверок сервиса источников: настоящий слой
// «Конфигурация» во временном каталоге рабочего каталога теста, настоящие
// бинарники ядер устройства (проверка `xray run -test` и `mihomo -t` идёт
// конвейером слоя). Вердикт — файлы на диске и ответы слоя, эталонов нет.

const (
	rtXrayBin   = "/opt/sbin/xray"
	rtMihomoBin = "/opt/sbin/mihomo"
	// rtUUIDA — UUID из данных ссылок testdata/links.
	rtUUIDA = "11111111-1111-4111-8111-111111111111"
	rtUUIDB = "33333333-3333-4333-8333-333333333333"
)

// rtClock — управляемое время сервиса (срок токена предпросмотра).
type rtClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *rtClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *rtClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// rtEnv — слой и сервис на каталоге теста.
type rtEnv struct {
	layer *configlayer.Layer
	svc   *Service
	roots configlayer.Roots
	clock *rtClock
	dir   string
	// installed — какие ядра видит сервис (и слой).
	installed configlayer.InstalledKernels
	// enabled — флаг config_layer, который опрашивает слой при каждом использовании.
	enabled atomic.Bool
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

// rtNeedKernels пропускает тест, если на устройстве нет нужных ядер.
func rtNeedKernels(t *testing.T, xray, mihomo bool) {
	t.Helper()
	if xray {
		if _, err := os.Stat(rtXrayBin); err != nil {
			t.Skipf("Xray не установлен на устройстве: %v", err)
		}
	}
	if mihomo {
		if _, err := os.Stat(rtMihomoBin); err != nil {
			t.Skipf("Mihomo не установлен на устройстве: %v", err)
		}
	}
}

// rtNewEnv создаёт слой с зарегистрированными генераторами источников и запускает
// его. Бинарники ядер подключаются, если они есть на устройстве и нужны (xray,
// mihomo); без них слой «видит» ядро неустановленным, а сервису ядра объявляются
// установленными по запросу (для проверок без применения).
func rtNewEnv(t *testing.T, xray, mihomo bool) *rtEnv {
	t.Helper()
	return rtNewEnvWith(t, xray, mihomo, nil)
}

// rtNewEnvWith — rtNewEnv с правкой зависимостей сервиса до создания (клиент с
// подсчётом настоящих запросов, свой HWID).
func rtNewEnvWith(t *testing.T, xray, mihomo bool, mutate func(*Deps)) *rtEnv {
	t.Helper()
	return rtNewEnvOpts(t, xray, mihomo, true, mutate)
}

// rtNewEnvOpts — rtNewEnvWith с начальным значением флага config_layer: выключенный
// слой при старте ничего не читает и не пишет.
func rtNewEnvOpts(t *testing.T, xray, mihomo, enabled bool, mutate func(*Deps)) *rtEnv {
	t.Helper()
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "src-")
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
	var bins configlayer.Binaries
	if xray {
		bins.Xray = rtXrayBin
	}
	if mihomo {
		bins.Mihomo = rtMihomoBin
	}
	e := &rtEnv{
		dir:       dir,
		roots:     roots,
		clock:     &rtClock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)},
		installed: configlayer.InstalledKernels{Xray: xray, Mihomo: mihomo},
	}
	e.enabled.Store(enabled)
	layer, err := configlayer.New(configlayer.Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       e.enabled.Load,
		Binaries:      func() configlayer.Binaries { return bins },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)
	e.layer = layer
	deps := Deps{
		DataDir:   dir,
		Layer:     layer,
		Installed: func() configlayer.InstalledKernels { return e.installed },
		Now:       e.clock.Now,
	}
	if mutate != nil {
		mutate(&deps)
	}
	svc, err := New(deps)
	if err != nil {
		t.Fatalf("sources.New: %v", err)
	}
	e.svc = svc
	// Сервис останавливается раньше слоя (cleanup идёт в обратном порядке).
	t.Cleanup(svc.Stop)
	for _, g := range svc.Generators() {
		if err := layer.RegisterGenerator(g); err != nil {
			t.Fatalf("RegisterGenerator %s: %v", g.ID(), err)
		}
	}
	if err := layer.AddLiveCommitter(svc.CommitLive); err != nil {
		t.Fatalf("AddLiveCommitter: %v", err)
	}
	if err := layer.OnDriftResolved(svc.OnDriftResolved); err != nil {
		t.Fatalf("OnDriftResolved: %v", err)
	}
	layer.Start()
	return e
}

// rev — текущая ревизия черновика.
func (e *rtEnv) rev(t *testing.T) int64 {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return st.DraftRevision
}

// addLinks кладёт узлы в «Мои узлы» через предпросмотр и «Добавить».
func (e *rtEnv) addLinks(t *testing.T, links ...string) {
	t.Helper()
	view, err := e.svc.Preview(context.Background(), inputOf(strings.Join(links, "\n")))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if view.Token == "" {
		t.Fatalf("предпросмотр без токена: %+v", view)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

// input строит ввод из текста.
func inputOf(text string) nodes.Input { return nodes.Input{Text: text} }

// applyAndWait запускает «Применить» и ждёт apply_done.
func (e *rtEnv) applyAndWait(t *testing.T) configlayer.ApplyView {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
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
				return view
			}
		case <-deadline:
			t.Fatal("«Применить» не завершилось за 3 минуты")
		}
	}
}

// draftInput возвращает секции черновика как вход генератора.
func (e *rtEnv) draftInput(t *testing.T) configlayer.Input {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return configlayer.Input{Sections: st.Draft, Live: st.Live, Mode: configlayer.SourceDraft}
}

// rtSection кодирует секцию src_my для прямой записи в черновик или входа генератора.
func rtSection(t *testing.T, cfg MySourceConfig) json.RawMessage {
	t.Helper()
	raw, err := encodeMy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rtGenerate вызывает генератор ядра по входу и возвращает его файлы.
func rtGenerate(t *testing.T, s *Service, kernel string, in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.GeneratedFile {
	t.Helper()
	for _, g := range s.Generators() {
		if (kernel == kernelXray && g.ID() == xrayGenID) || (kernel == kernelMihomo && g.ID() == mihomoGenID) {
			files, err := g.Generate(in, installed)
			if err != nil {
				t.Fatalf("генератор %s: %v", g.ID(), err)
			}
			return files
		}
	}
	t.Fatalf("нет генератора для ядра %s", kernel)
	return nil
}
