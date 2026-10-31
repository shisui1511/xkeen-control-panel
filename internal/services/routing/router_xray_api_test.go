//go:build router

package routing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Блок api Xray — отдельный файл панели 99_xcp-api.json (143 Q5): api.listen на
// loopback и сервисы, которые нужны панели, без входящего, правила api, policy и
// routing. Файл пишется вместе с маршрутизацией панели, только при отсутствии
// чужого api, и работает при правилах routing в другом файле. Проверяется
// настоящими `xray run -test` и изолированным процессом Xray с файлами
// генератора; рабочее ядро стенда не трогается (Applier не задан), ядро тест не
// читает и не переключает — метка ядра не нужна.

// rtxaOldAPIFile — 00_api.json старого слоя: api без listen, входящий
// dokodemo-door tag api и правило api в routing.
const rtxaOldAPIFile = `{
  "stats": {},
  "api": {"tag": "api", "services": ["StatsService", "RoutingService", "LoggerService"]},
  "inbounds": [{"tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}],
  "routing": {"rules": [{"type": "field", "inboundTag": ["api"], "outboundTag": "api"}]}
}
`

// rtxaForeignRules — чужой файл с правилами routing (случай P4: правила лежат
// не там, где api).
const rtxaForeignRules = `{
  "routing": {"rules": [{"type": "field", "domain": ["domain:extra.example.test"], "outboundTag": "` + TagDirect + `"}]}
}
`

// rtxaServices читает services блока api из файла панели.
func rtxaServices(t *testing.T, e *rtEnv) (api map[string]any, services []string) {
	t.Helper()
	doc := e.xrayDoc(t, configlayer.PanelXrayAPIFile)
	api, _ = doc["api"].(map[string]any)
	if api == nil {
		t.Fatalf("в %s нет объекта api: %v", configlayer.PanelXrayAPIFile, doc)
	}
	return api, rtxStrings(api["services"])
}

// rtxaRun выполняет команду xray и возвращает вывод; ненулевой код — провал.
func rtxaRun(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rtXrayBin, args...)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray %v: %v: %s", args[:2], err, rtRedact(string(out)))
	}
	return string(out)
}

// rtxaCopyDir копирует *.json файлов панели в новый каталог теста.
func rtxaCopyDir(t *testing.T, from string) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-api-")
	rtCopyFiles(t, from, dir)
	return dir
}

func TestRouterXrayAPIBlock(t *testing.T) {
	rtNeedBins(t, rtXrayBin)
	apiPort := rtFreePort(t)
	newEnv := func(t *testing.T) *rtEnv {
		t.Helper()
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
			d.XrayAPIPort = func() int { return apiPort }
		}))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		return e
	}

	t.Run("panel_api_works_with_rules_elsewhere", func(t *testing.T) {
		e := newEnv(t)
		e.takeAndApply(t)

		api, svcs := rtxaServices(t, e)
		if api["tag"] != "api" || api["listen"] != "127.0.0.1:"+strconv.Itoa(apiPort) {
			t.Errorf("api: tag=%v listen=%v, ожидались api и 127.0.0.1:%d", api["tag"], api["listen"], apiPort)
		}
		want := []string{"HandlerService", "LoggerService", "ObservatoryService", "RoutingService", "StatsService"}
		if !rtxEqualStrings(svcs, want) {
			t.Errorf("services %v, ожидались %v (группа «авто» даёт наблюдение)", svcs, want)
		}
		doc := e.xrayDoc(t, configlayer.PanelXrayAPIFile)
		if _, has := doc["stats"]; !has {
			t.Errorf("в файле api нет stats: %v", doc)
		}
		for _, k := range []string{"inbounds", "routing", "policy"} {
			if _, has := doc[k]; has {
				t.Errorf("в файле api есть ключ %s: замена объекта целиком стёрла бы чужие значения", k)
			}
		}
		for _, r := range rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))) {
			if rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{"api"}) {
				t.Errorf("в routing панели есть правило api: %v", r)
			}
		}

		// Правила routing — в другом файле, api остаётся рабочим.
		dir := rtxaCopyDir(t, e.xrayDir())
		if err := os.WriteFile(filepath.Join(dir, "05_extra.json"), []byte(rtxaForeignRules), 0o600); err != nil {
			t.Fatal(err)
		}
		rtxaRun(t, "run", "-test", "-confdir", dir)

		// Изолированный процесс: слушает только API на loopback.
		rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), apiPort)
		server := "127.0.0.1:" + strconv.Itoa(apiPort)
		rtxaRun(t, "api", "lso", "-s", server)
		rtxaRun(t, "api", "statssys", "-s", server)

		info := services.FindXrayAPIFragment(dir)
		if !info.APIPresent || filepath.Base(info.Path) != configlayer.PanelXrayAPIFile {
			t.Errorf("FindXrayAPIFragment: present=%v файл=%s, ожидался %s", info.APIPresent, filepath.Base(info.Path), configlayer.PanelXrayAPIFile)
		}
	})

	t.Run("foreign_api_keeps_old_rule", func(t *testing.T) {
		e := newEnv(t)
		if err := os.WriteFile(filepath.Join(e.xrayDir(), "00_api.json"), []byte(rtxaOldAPIFile), 0o600); err != nil {
			t.Fatal(err)
		}
		e.takeAndApply(t)

		if e.xrayFileExists(configlayer.PanelXrayAPIFile) {
			t.Errorf("при чужом api панель записала %s: у ключа api должен быть один владелец", configlayer.PanelXrayAPIFile)
		}
		rules := rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile)))
		if len(rules) == 0 || !rtxEqualStrings(rtxStrings(rules[0]["inboundTag"]), []string{"api"}) || rules[0]["outboundTag"] != "api" {
			t.Errorf("первое правило routing панели %v, ожидалось inboundTag [api] → outboundTag api", rules)
		}
		rtxaRun(t, "run", "-test", "-confdir", rtxaCopyDir(t, e.xrayDir()))
		info := services.FindXrayAPIFragment(e.xrayDir())
		if !info.APIPresent || filepath.Base(info.Path) != "00_api.json" {
			t.Errorf("FindXrayAPIFragment: present=%v файл=%s, ожидался 00_api.json", info.APIPresent, filepath.Base(info.Path))
		}
	})

	t.Run("no_observatory_service_without_observation", func(t *testing.T) {
		e := newEnv(t)
		// Все группы — ручные: balancer-ов и файла наблюдения нет.
		e.replaceGroups(t, func(cfg *GroupsConfig) {
			for i := range cfg.Items {
				cfg.Items[i].Mode = ModeManual
				cfg.Items[i].Members = []Member{{Kind: KindDirect}}
			}
		})
		e.takeAndApply(t)

		if e.xrayFileExists(xrayObservatoryFile) {
			t.Errorf("файл наблюдения %s записан без групп «авто» и «резерв»", xrayObservatoryFile)
		}
		_, svcs := rtxaServices(t, e)
		want := []string{"HandlerService", "LoggerService", "RoutingService", "StatsService"}
		if !rtxEqualStrings(svcs, want) {
			t.Errorf("services %v, ожидались %v (без наблюдения ObservatoryService не нужен)", svcs, want)
		}
		rtxaRun(t, "run", "-test", "-confdir", rtxaCopyDir(t, e.xrayDir()))
	})
}
