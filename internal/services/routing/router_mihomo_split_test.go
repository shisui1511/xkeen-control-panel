//go:build router

package routing

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Разделение владения маршрутизацией по ядрам (147.1, D-06, D-11): настоящий слой,
// настоящие `xray run -test` и `mihomo -t` устройства, файлы панели читаются с
// диска. Ядро стенда не трогается (Applier не задан), ядро тест не читает и не
// переключает — метка ядра не нужна.

const (
	rtsBoth      = `{"v":1,"managed":true}`
	rtsMihomoOff = `{"v":1,"managed":true,"mihomo":false}`
)

// rtsOwnerEnv — окружение с обоими ядрами, прежним профилем Mihomo, «Моими узлами»
// и, если на стенде есть списки dat, правилом со списком (оно даёт файлы
// rule_providers/xcp-*.txt). Возвращает окружение и признак наличия списков.
func rtsOwnerEnv(t *testing.T) (*rtEnv, bool) {
	t.Helper()
	ix := lists.NewIndex("")
	site, _ := rtmDatRefs(ix)
	e := rtNewEnv(t, rtWithDeps(func(d *Deps) { d.Lists = ix }))
	e.writeUserProfile(t)
	e.addMyLinks(t, "vless_ws")
	if site != nil {
		rule := Rule{Enabled: true, Target: Target{Kind: KindDirect}, Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{*site}}}}
		if _, _, err := e.svc.SaveRule(e.rev(t), rule); err != nil {
			t.Fatalf("SaveRule (список): %v", err)
		}
	}
	return e, site != nil
}

// rtsRuleProviderFiles перечисляет rule_providers/xcp-*.txt на диске Mihomo.
func (e *rtEnv) rtsRuleProviderFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(e.mihomoPath("rule_providers/xcp-*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// rtsManifestInert читает признак Inert записи манифеста из файла состояния слоя.
func (e *rtEnv) rtsManifestInert(t *testing.T, kernel, rel string) (inert, found bool) {
	t.Helper()
	e.layer.FlushState()
	data, err := os.ReadFile(filepath.Join(e.dir, configlayer.StateFileName))
	if err != nil {
		t.Fatalf("файл состояния слоя: %v", err)
	}
	var st struct {
		Manifest map[string]struct {
			Inert bool `json:"inert"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("файл состояния слоя не JSON: %v", err)
	}
	entry, ok := st.Manifest[configlayer.ManifestKey(kernel, rel)]
	return entry.Inert, ok
}

func TestRouterMihomoSplitGenerators(t *testing.T) {
	rtNeedKernels(t)

	// D-11: пока Mihomo не под управлением панели, профиль панели и провайдеры правил
	// не генерируются и удаляются применением; провайдер узлов остаётся Inert, файлы
	// Xray на месте.
	t.Run("mihomo_off_keeps_xray", func(t *testing.T) {
		e, hasLists := rtsOwnerEnv(t)
		e.takeAndApply(t)
		if _, err := os.Stat(e.mihomoPath(configlayer.PanelMihomoProfile)); err != nil {
			t.Fatalf("профиль панели после взятия: %v", err)
		}
		if hasLists && len(e.rtsRuleProviderFiles(t)) == 0 {
			t.Fatal("после взятия нет файлов rule_providers/xcp-*.txt")
		}
		if !e.xrayFileExists(configlayer.PanelXrayRoutingFile) {
			t.Fatalf("после взятия нет %s", configlayer.PanelXrayRoutingFile)
		}

		e.putSection(t, configlayer.RoutingSection, json.RawMessage(rtsMihomoOff))
		e.applyOK(t)

		if _, err := os.Lstat(e.mihomoPath(configlayer.PanelMihomoProfile)); !os.IsNotExist(err) {
			t.Errorf("профиль панели остался на диске: %v", err)
		}
		if left := e.rtsRuleProviderFiles(t); len(left) != 0 {
			t.Errorf("остались файлы провайдеров правил: %v", left)
		}
		if _, err := os.Stat(e.mihomoPath("proxy_providers/xcp-my.yaml")); err != nil {
			t.Errorf("провайдер «Моих узлов» пропал: %v", err)
		}
		if inert, found := e.rtsManifestInert(t, configlayer.KernelMihomo, "proxy_providers/xcp-my.yaml"); !found || !inert {
			t.Errorf("запись манифеста xcp-my.yaml: найдена=%v inert=%v, ожидалась inert", found, inert)
		}
		if !e.xrayFileExists(configlayer.PanelXrayRoutingFile) {
			t.Errorf("файл %s Xray пропал при отпускании Mihomo", configlayer.PanelXrayRoutingFile)
		}
		st := e.state(t)
		if got := configlayer.DesiredRouting(st.Applied); !got.ManagedFor(configlayer.KernelXray) || got.ManagedFor(configlayer.KernelMihomo) {
			t.Errorf("применённая секция routing: %+v", got)
		}
	})

	// Дрейф по ядрам: расхождение Mihomo не блокирует запуск, который отпускает
	// Mihomo; расхождение Xray блокирует запуск, который держит Xray.
	t.Run("drift_mihomo_released", func(t *testing.T) {
		e, _ := rtsOwnerEnv(t)
		e.takeAndApply(t)
		// config.yaml переложен руками мимо панели.
		cfg := e.mihomoPath("config.yaml")
		if err := os.Remove(cfg); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(e.mihomoDir(), "profiles", "user.yaml"), cfg); err != nil {
			t.Fatal(err)
		}
		e.putSection(t, configlayer.RoutingSection, json.RawMessage(rtsMihomoOff))
		res, err := e.applyResult(t)
		if errors.Is(err, configlayer.ErrDriftBlocked) {
			t.Fatalf("дрейф Mihomo заблокировал запуск, отпускающий Mihomo: %v", err)
		}
		if err != nil {
			t.Fatalf("«Применить» отклонено: %v", err)
		}
		if !res.OK {
			t.Fatalf("«Применить»: code=%q issues=%+v", res.Code, res.Issues)
		}
	})

	t.Run("drift_xray_blocks", func(t *testing.T) {
		e, _ := rtsOwnerEnv(t)
		e.takeAndApply(t)
		// Чужой файл routing появился в configs/ после взятия.
		foreign := filepath.Join(e.xrayDir(), "05_routing.json")
		if err := os.WriteFile(foreign, []byte(`{"routing":{"rules":[]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		e.putSection(t, configlayer.RoutingSection, json.RawMessage(rtsMihomoOff))
		_, err := e.applyResult(t)
		if !errors.Is(err, configlayer.ErrDriftBlocked) {
			t.Fatalf("StartApply при дрейфе Xray, который панель держит: %v, ожидался ErrDriftBlocked", err)
		}
	})
}

// TestRouterMihomoSplitChecks — проверка черновика считает только ядра под
// управлением (D-06): force-proxy ядра, которое панель не держит, не блокирует
// «Применить». Диск и ядра стенда не трогаются, ядро тест не читает и не
// переключает — метка ядра не нужна.
func TestRouterMihomoSplitChecks(t *testing.T) {
	rtNeedKernels(t)

	e := rtNewEnv(t)
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
	rtmsWriteProfile(t, e, rtfpMihomoProfile)
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	// Обе секции входящих ссылаются на группу, которой нет.
	xrayInbounds := json.RawMessage(`[{"tag":"redirect","listen":"127.0.0.1","port":61219,"protocol":"dokodemo-door",` +
		`"settings":{"network":"tcp","followRedirect":true}},` +
		`{"tag":"force-proxy-redirect","listen":"127.0.0.1","port":61221,"protocol":"dokodemo-door",` +
		`"settings":{"network":"tcp","followRedirect":true}}]`)
	raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: true, Value: xrayInbounds, ForceProxyGroup: "deleted-group-id"})
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, configlayer.SectionXrayInbounds, raw)
	cs := rtmsSection(t, e.state(t).Draft, "inbounds")
	cs.ForceProxyGroup = "deleted-group-id"
	raw, err = configlayer.EncodeCoreSection(cs)
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, configlayer.SectionMihomoInbounds, raw)

	cases := []struct {
		name       string
		routing    string
		wantXray   bool
		wantMihomo bool
	}{
		{"both_managed", rtsBoth, true, true},
		{"mihomo_own_profile", rtsMihomoOff, true, false},
		{"only_mihomo", `{"v":1,"managed":false,"mihomo":true}`, false, true},
		{"released", `{"v":1,"managed":false}`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.putSection(t, configlayer.RoutingSection, json.RawMessage(c.routing))
			issues := e.check(t)
			if got := rtfpHasIssue(issues, configlayer.SectionXrayInbounds); got != c.wantXray {
				t.Errorf("проблема force-proxy Xray: %v, ожидалось %v (%+v)", got, c.wantXray, issues)
			}
			if got := rtfpHasIssue(issues, configlayer.SectionMihomoInbounds); got != c.wantMihomo {
				t.Errorf("проблема force-proxy Mihomo: %v, ожидалось %v (%+v)", got, c.wantMihomo, issues)
			}
		})
	}
}
