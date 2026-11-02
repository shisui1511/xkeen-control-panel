//go:build router

package routing

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Область взятия маршрутизации (147.1, D-07): настоящий слой, настоящие
// `xray run -test` и `mihomo -t` устройства, файлы читаются с диска. Ядро стенда
// не трогается (Applier не задан), ядро тест не читает и не переключает — метка
// ядра не нужна.

// foreignXrayRouting — чужой файл routing Xray, который область mihomo не должна
// ни переносить в копии, ни менять.
const foreignXrayRouting = `{"routing":{"rules":[]}}`

// rtscForeignFile кладёт чужой файл routing в каталог Xray и возвращает его путь.
func rtscForeignFile(t *testing.T, e *rtEnv) string {
	t.Helper()
	path := filepath.Join(e.xrayDir(), "05_routing.json")
	if err := os.WriteFile(path, []byte(foreignXrayRouting), 0o644); err != nil {
		t.Fatalf("чужой файл routing: %v", err)
	}
	return path
}

// rtscSameFile требует, чтобы файл существовал и совпадал с исходным побайтно.
func rtscSameFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("файл %s: %v", filepath.Base(path), err)
		return
	}
	if !bytes.Equal(data, []byte(foreignXrayRouting)) {
		t.Errorf("файл %s изменён", filepath.Base(path))
	}
}

// rtscRouting читает секцию routing черновика или применённого состояния.
func rtscRouting(t *testing.T, sections configlayer.Sections) configlayer.RoutingDesired {
	t.Helper()
	d, err := configlayer.ParseRoutingDesired(sections[configlayer.RoutingSection])
	if err != nil {
		t.Fatalf("секция routing: %v", err)
	}
	return d
}

// rtscConfigLink возвращает цель симлинка config.yaml Mihomo.
func rtscConfigLink(t *testing.T, e *rtEnv) string {
	t.Helper()
	link, err := os.Readlink(e.mihomoPath("config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml: %v", err)
	}
	return link
}

func TestRouterMihomoScopedTake(t *testing.T) {
	rtNeedKernels(t)

	// Взять только Mihomo: config.yaml на профиле панели, Xray за XKeen, чужой
	// файл routing и секция probe на месте; «Отпустить» возвращает прежний профиль.
	t.Run("take_mihomo_only", func(t *testing.T) {
		e := rtNewEnv(t)
		user := e.writeUserProfile(t)
		foreign := rtscForeignFile(t, e)
		probeBefore := string(e.state(t).Draft[ProbeSection])

		if _, err := e.svc.TakeScoped(e.rev(t), ScopeMihomo); err != nil {
			t.Fatalf("TakeScoped(mihomo): %v", err)
		}
		d := rtscRouting(t, e.state(t).Draft)
		if !d.ManagedFor(configlayer.KernelMihomo) || d.ManagedFor(configlayer.KernelXray) {
			t.Fatalf("секция routing после взятия Mihomo: %+v", d)
		}
		e.applyOK(t)

		if got, err := filepath.EvalSymlinks(e.mihomoPath("config.yaml")); err != nil || got != mustEval(t, e.mihomoPath(configlayer.PanelMihomoProfile)) {
			t.Errorf("config.yaml указывает на %q (%v), ожидался профиль панели", got, err)
		}
		snap := e.layer.Snapshot().Routing
		if snap.Xray != nil {
			t.Errorf("Xray взят панелью: %+v", snap.Xray)
		}
		if snap.Mihomo == nil || !snap.Mihomo.Taken {
			t.Errorf("Mihomo не взят: %+v", snap.Mihomo)
		}
		rtscSameFile(t, foreign)
		if e.xrayFileExists(configlayer.PanelXrayRoutingFile) {
			t.Errorf("файл %s появился при взятии только Mihomo", configlayer.PanelXrayRoutingFile)
		}
		if got := string(e.state(t).Draft[ProbeSection]); got != probeBefore {
			t.Error("секция probe изменена взятием только Mihomo")
		}

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		e.applyOK(t)
		if link := rtscConfigLink(t, e); link != user {
			t.Errorf("config.yaml после Release: %q, ожидался прежний профиль", link)
		}
		if snap := e.layer.Snapshot().Routing; snap.Xray != nil || (snap.Mihomo != nil && snap.Mihomo.Taken) {
			t.Errorf("после Release владение осталось: %+v", snap)
		}
		rtscSameFile(t, foreign)
	})
}
