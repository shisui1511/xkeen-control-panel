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

// rtscUserXray кладёт пользовательский файл исходящих в каталог Xray: у живого
// Xray он есть всегда, а `xray run -test` на пустом каталоге отказывает, и
// «Применить» после возврата файлов панели не прошло бы.
func rtscUserXray(t *testing.T, e *rtEnv) {
	t.Helper()
	const outbounds = `{"outbounds":[{"tag":"direct","protocol":"freedom"}]}`
	if err := os.WriteFile(filepath.Join(e.xrayDir(), "04_outbounds.json"), []byte(outbounds), 0o644); err != nil {
		t.Fatalf("исходящие Xray: %v", err)
	}
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

// rtscTaken — ядро взято панелью: снимок отдаёт вид и для невзятого ядра
// (Taken=false), поэтому nil-проверки мало.
func rtscTaken(v *configlayer.RoutingKernelView) bool { return v != nil && v.Taken }

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
		if rtscTaken(snap.Xray) {
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
		if snap := e.layer.Snapshot().Routing; rtscTaken(snap.Xray) || rtscTaken(snap.Mihomo) {
			t.Errorf("после Release владение осталось: %+v", snap)
		}
		rtscSameFile(t, foreign)
	})

	// Mihomo на своём профиле, берётся только Xray: config.yaml и секции Mihomo не
	// меняются, секция routing запоминает решение по Mihomo.
	t.Run("own_profile_scope_xray", func(t *testing.T) {
		e := rtNewEnv(t)
		user := e.writeUserProfile(t)

		if _, err := e.svc.TakeScoped(e.rev(t), ScopeXray); err != nil {
			t.Fatalf("TakeScoped(xray): %v", err)
		}
		d := rtscRouting(t, e.state(t).Draft)
		if !d.Managed || d.Mihomo == nil || *d.Mihomo {
			t.Fatalf("секция routing после взятия Xray: managed=%v mihomo=%v, ожидались managed=true, mihomo=false", d.Managed, d.Mihomo)
		}
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, e.state(t).Draft, id); cs.Managed {
				t.Errorf("секция mihomo_%s под управлением после взятия только Xray", id)
			}
		}
		e.applyOK(t)

		if link := rtscConfigLink(t, e); link != user {
			t.Errorf("config.yaml после взятия Xray: %q, ожидался прежний профиль", link)
		}
		if _, err := os.Stat(e.mihomoPath(configlayer.PanelMihomoProfile)); err == nil {
			t.Error("профиль панели появился при взятии только Xray")
		}
		snap := e.layer.Snapshot().Routing
		if snap.Xray == nil || !snap.Xray.Taken {
			t.Errorf("Xray не взят: %+v", snap.Xray)
		}
		if snap.Mihomo != nil && snap.Mihomo.Taken {
			t.Errorf("Mihomo взят: %+v", snap.Mihomo)
		}
		if !e.xrayFileExists(configlayer.PanelXrayRoutingFile) {
			t.Errorf("нет файла %s", configlayer.PanelXrayRoutingFile)
		}
	})

	// «Отпустить» с «Групп» и «Правил» не снимает решение «Mihomo на своём профиле»:
	// «Взять всё» тоже оставляет Mihomo на своём профиле (D-07).
	t.Run("release_keeps_mihomo_off", func(t *testing.T) {
		e := rtNewEnv(t)
		rtscUserXray(t, e)
		user := e.writeUserProfile(t)
		if _, err := e.svc.TakeScoped(e.rev(t), ScopeXray); err != nil {
			t.Fatalf("TakeScoped(xray): %v", err)
		}
		e.applyOK(t)

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		d := rtscRouting(t, e.state(t).Draft)
		if d.Managed || d.Mihomo == nil || *d.Mihomo {
			t.Fatalf("секция routing после Release: managed=%v mihomo=%v, ожидались managed=false, mihomo=false", d.Managed, d.Mihomo)
		}
		e.applyOK(t)
		if link := rtscConfigLink(t, e); link != user {
			t.Errorf("config.yaml после Release: %q, ожидался прежний профиль", link)
		}
		if e.xrayFileExists(configlayer.PanelXrayRoutingFile) {
			t.Errorf("файл %s остался после Release", configlayer.PanelXrayRoutingFile)
		}
		if snap := e.layer.Snapshot().Routing; rtscTaken(snap.Xray) {
			t.Errorf("Xray остался взятым: %+v", snap.Xray)
		}

		// Взять всё: Mihomo остаётся на своём профиле (D-07), Xray взят.
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		d = rtscRouting(t, e.state(t).Draft)
		if !d.Managed || d.ManagedFor(configlayer.KernelMihomo) {
			t.Fatalf("секция routing после Take: managed=%v mihomo=%v, ожидались managed=true, Mihomo не взят", d.Managed, d.Mihomo)
		}
		e.applyOK(t)
		if link := rtscConfigLink(t, e); link != user {
			t.Errorf("config.yaml после Take: %q, Mihomo ушёл со своего профиля", link)
		}
		if snap := e.layer.Snapshot().Routing; snap.Xray == nil || !snap.Xray.Taken {
			t.Errorf("Xray не взят после Take: %+v", snap.Xray)
		}
	})

	// Оба ядра у панели: «Отпустить» возвращает оба, значения секций Mihomo
	// сохраняются в черновике и в применённом состоянии (D-09).
	t.Run("all_when_both_panel", func(t *testing.T) {
		e := rtNewEnv(t)
		rtscUserXray(t, e)
		user := e.writeUserProfile(t)
		e.takeAndApply(t)
		snap := e.layer.Snapshot().Routing
		if snap.Xray == nil || !snap.Xray.Taken || snap.Mihomo == nil || !snap.Mihomo.Taken {
			t.Fatalf("после Take взяты не оба ядра: xray=%+v mihomo=%+v", snap.Xray, snap.Mihomo)
		}

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		e.applyOK(t)
		snap = e.layer.Snapshot().Routing
		if rtscTaken(snap.Xray) || rtscTaken(snap.Mihomo) {
			t.Errorf("после Release остались взятые ядра: xray=%+v mihomo=%+v", snap.Xray, snap.Mihomo)
		}
		if link := rtscConfigLink(t, e); link != user {
			t.Errorf("config.yaml после Release: %q, ожидался прежний профиль", link)
		}
		st := e.state(t)
		for _, id := range configlayer.MihomoSectionIDs {
			for name, sections := range map[string]configlayer.Sections{"черновик": st.Draft, "применённое": st.Applied} {
				if cs := rtmsSection(t, sections, id); cs.Managed || len(cs.Value) == 0 {
					t.Errorf("секция mihomo_%s (%s) после Release: managed=%v value=%d, ожидались managed=false и сохранённое значение",
						id, name, cs.Managed, len(cs.Value))
				}
			}
		}
	})
}
