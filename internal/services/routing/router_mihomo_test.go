//go:build router

package routing

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Материализация групп, правил и списков в Mihomo: настоящий слой, настоящий
// `mihomo -t` устройства в конвейере «Применить» (профиль панели проверяется как
// config.yaml), файлы панели читаются с диска. Ядро стенда не трогается
// (Applier не задан), ядро тест не читает и не переключает — метка ядра не нужна.
// Трафик, fail-closed и точечный PUT провайдеров — сценарии 146-25.

// rtMihomoOnly — установлен только Mihomo.
var rtMihomoOnly = configlayer.InstalledKernels{Mihomo: true}

// rtmUserProfile — прежний активный профиль: порты 7000 и 7001 панельный профиль
// обязан повторить (D-05).
const rtmUserProfile = "mode: rule\n" +
	"redir-port: 7000\n" +
	"tproxy-port: 7001\n" +
	"allow-lan: true\n" +
	"proxy-groups:\n" +
	"  - name: PROXY\n" +
	"    type: select\n" +
	"    proxies:\n" +
	"      - DIRECT\n" +
	"rules:\n" +
	"  - MATCH,PROXY\n"

// mihomoDir — каталог файлов Mihomo слоя окружения.
func (e *rtEnv) mihomoDir() string { return filepath.Join(e.dir, "mihomo") }

// writeUserProfile кладёт прежний профиль profiles/user.yaml и симлинк config.yaml
// на него (как у пользователя XKeen). Возвращает путь прежнего профиля.
func (e *rtEnv) writeUserProfile(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(e.mihomoDir(), "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(dir, "user.yaml")
	if err := os.WriteFile(user, []byte(rtmUserProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(user, filepath.Join(e.mihomoDir(), "config.yaml")); err != nil {
		t.Fatal(err)
	}
	return user
}

// mihomoPath — путь файла панели в каталоге Mihomo.
func (e *rtEnv) mihomoPath(rel string) string {
	return filepath.Join(e.mihomoDir(), filepath.FromSlash(rel))
}

// mihomoProfile читает профиль панели как YAML-объект.
func (e *rtEnv) mihomoProfile(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		t.Fatalf("профиль панели: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("профиль панели не YAML: %v", err)
	}
	return doc
}

// rtmStrings приводит YAML-список строк к []string.
func rtmStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// rtmGroups индексирует proxy-groups профиля по имени.
func rtmGroups(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()
	list, _ := doc["proxy-groups"].([]any)
	out := make(map[string]map[string]any, len(list))
	for _, x := range list {
		g, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("группа профиля не объект: %T", x)
		}
		name, _ := g["name"].(string)
		if _, dup := out[name]; dup {
			t.Errorf("имя группы %q повторено", name)
		}
		out[name] = g
	}
	return out
}

// rtmCountLines считает непустые строки файла.
func rtmCountLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл провайдера: %v", err)
	}
	n := 0
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// Группы и правила черновика после «Применить» со взятием превращаются в профиль
// панели Mihomo секциями и проходят `mihomo -t`; возврат убирает профиль.
func TestRouterMihomoGroupsMaterialize(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
	user := e.writeUserProfile(t)
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")

	catalog := e.catalog(t).Mihomo
	if len(catalog) < 3 {
		t.Fatalf("в каталоге Mihomo %d узлов, ожидалось 3", len(catalog))
	}
	reserveNode := catalog[0]

	cfg := e.groups(t)
	auto, okA := rtGroupByName(cfg, "Авто")
	proxyG, okP := rtGroupByName(cfg, "Прокси")
	if !okA || !okP {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	_, svcGroup, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Сервис", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: proxyG.ID}, {Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Сервис): %v", err)
	}
	_, reserveG, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Резерв", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: reserveNode.Ref,
	})
	if err != nil {
		t.Fatalf("SaveGroup (Резерв): %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"direct.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule (домен → DIRECT): %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindReject},
		Conds: []Cond{{Type: CondNetwork, Values: []string{"udp"}}, {Type: CondPort, Values: []string{"443"}}},
	}); err != nil {
		t.Fatalf("SaveRule (udp 443 → REJECT): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: proxyG.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: rtxDefaultProbeURL, IntervalS: 20, TimeoutS: 5}); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}

	// «Применить» со взятием: проверка настоящим mihomo -t на итоговой раскладке,
	// где config.yaml указывает на профиль панели.
	e.takeAndApply(t)

	doc := e.mihomoProfile(t)
	if doc["redir-port"] != 7000 || doc["tproxy-port"] != 7001 {
		t.Errorf("порты профиля %v/%v, ожидались 7000/7001 прежнего профиля", doc["redir-port"], doc["tproxy-port"])
	}
	if doc["routing-mark"] != 255 || doc["mode"] != "rule" {
		t.Errorf("routing-mark %v, mode %v", doc["routing-mark"], doc["mode"])
	}
	if sock, _ := doc["external-controller-unix"].(string); sock == "" {
		t.Error("в профиле нет external-controller-unix")
	}
	if prof, _ := doc["profile"].(map[string]any); prof["store-selected"] != true {
		t.Errorf("profile.store-selected: %v", doc["profile"])
	}
	for _, key := range []string{"geox-url", "geo-auto-update", "geodata-mode", "geodata-loader", "geo-update-interval"} {
		if _, has := doc[key]; has {
			t.Errorf("в профиле есть ключ автозагрузки geodata %s (D-05)", key)
		}
	}
	if sn, _ := doc["sniffer"].(map[string]any); sn["enable"] != true || sn["override-destination"] != false {
		t.Errorf("sniffer: %v", doc["sniffer"])
	}
	if dns, _ := doc["dns"].(map[string]any); dns["enable"] != true {
		t.Errorf("dns: %v", doc["dns"])
	}
	if _, err := os.Stat(e.mihomoPath("proxy_providers/xcp-my.yaml")); err != nil {
		t.Errorf("файл провайдера «Моих узлов»: %v", err)
	}
	provs, _ := doc["proxy-providers"].(map[string]any)
	if my, _ := provs["xcp-my"].(map[string]any); my["type"] != "file" || my["path"] != "./proxy_providers/xcp-my.yaml" {
		t.Errorf("proxy-providers.xcp-my: %v", provs["xcp-my"])
	}

	groups := rtmGroups(t, doc)
	hidden := hiddenName(reserveG.ID)
	for name, g := range groups {
		if g["empty-fallback"] != "REJECT" {
			t.Errorf("у группы %q empty-fallback %v, ожидался REJECT (пустая группа не должна уходить в DIRECT)", name, g["empty-fallback"])
		}
	}
	if g := groups["Прокси"]; g == nil || g["type"] != "select" ||
		!rtxEqualStrings(rtmStrings(g["proxies"]), []string{"Авто"}) || !rtxEqualStrings(rtmStrings(g["use"]), []string{"xcp-my"}) {
		t.Errorf("группа «Прокси»: %v (ожидались select, первый участник «Авто», use [xcp-my])", g)
	}
	if g := groups["Авто"]; g == nil || g["type"] != "url-test" || !rtxEqualStrings(rtmStrings(g["use"]), []string{"xcp-my"}) ||
		g["url"] != rtxDefaultProbeURL || g["interval"] != 20 || g["timeout"] != 5000 || g["tolerance"] != 50 || g["lazy"] != true {
		t.Errorf("группа «Авто»: %v", g)
	}
	if g := groups[svcGroup.Name]; g == nil || g["type"] != "select" ||
		!rtxEqualStrings(rtmStrings(g["proxies"]), []string{"Прокси", "Авто", "DIRECT"}) {
		t.Errorf("группа «Сервис»: %v (ожидались участники «Прокси», «Авто», DIRECT)", g)
	}
	// «Резерв»: скрытая url-test по основным узлам (без резервного) и резервная копия.
	proxies, _ := doc["proxies"].([]any)
	var copyNames []string
	for _, x := range proxies {
		p, _ := x.(map[string]any)
		name, _ := p["name"].(string)
		copyNames = append(copyNames, name)
	}
	if !rtxEqualStrings(copyNames, []string{reserveNode.MihomoName}) {
		t.Errorf("копии узлов в proxies: %v, ожидалась одна — резервный узел %q", copyNames, reserveNode.MihomoName)
	}
	if g := groups["Резерв"]; g == nil || g["type"] != "fallback" ||
		!rtxEqualStrings(rtmStrings(g["proxies"]), []string{hidden, reserveNode.MihomoName}) {
		t.Errorf("группа «Резерв»: %v", g)
	}
	if g := groups[hidden]; g == nil || g["type"] != "url-test" || g["hidden"] != true ||
		!rtxEqualStrings(rtmStrings(g["use"]), []string{"xcp-my"}) ||
		g["exclude-filter"] != "^"+regexp.QuoteMeta(reserveNode.MihomoName)+"$" {
		t.Errorf("скрытая группа %s: %v", hidden, g)
	}

	want := []string{
		"DOMAIN-SUFFIX,direct.example.test,DIRECT",
		"AND,((NETWORK,UDP),(DST-PORT,443)),REJECT",
		"MATCH,Прокси",
	}
	if got := rtmStrings(doc["rules"]); !rtxEqualStrings(got, want) {
		t.Errorf("rules профиля %q, ожидались %q", got, want)
	}

	// Возврат: профиля панели нет, config.yaml снова указывает на прежний профиль.
	if _, err := e.svc.Release(e.rev(t)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	e.applyOK(t)
	if _, err := os.Stat(e.mihomoPath(configlayer.PanelMihomoProfile)); err == nil {
		t.Error("после возврата профиль панели остался на диске")
	}
	if link, err := os.Readlink(e.mihomoPath("config.yaml")); err != nil || link != user {
		t.Errorf("config.yaml после возврата: %q, %v (ожидался симлинк на прежний профиль)", link, err)
	}
}
