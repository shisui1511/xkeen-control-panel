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
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
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

// rtmDatRefs находит в каталоге dat стенда первый небольшой список geosite и
// geoip (не больше трёх тысяч записей, чтобы не упереться в пределы Mihomo);
// nil — списка нет.
func rtmDatRefs(ix *lists.Index) (site, ip *lists.ListRef) {
	files, err := ix.Files()
	if err != nil {
		return nil, nil
	}
	for _, f := range files {
		if (f.Kind != lists.KindSite && f.Kind != lists.KindIP) || !listFileRE.MatchString(f.Name) {
			continue
		}
		if (f.Kind == lists.KindSite && site != nil) || (f.Kind == lists.KindIP && ip != nil) {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil {
			continue
		}
		for _, tg := range tags {
			if !listTagRE.MatchString(tg.Tag) || tg.Count < 3 || tg.Count > 3000 {
				continue
			}
			ref := lists.ListRef{File: f.Name, Tag: tg.Tag, Kind: f.Kind}
			if f.Kind == lists.KindSite {
				site = &ref
			} else {
				ip = &ref
			}
			break
		}
		if site != nil && ip != nil {
			break
		}
	}
	return site, ip
}

// Правила и списки dat в Mihomo: строки rules, свои текстовые провайдеры правил с
// теми же данными, что видит Xray, пороги версий.
func TestRouterMihomoRulesAndLists(t *testing.T) {
	t.Run("lists_and_rules", func(t *testing.T) {
		ix := lists.NewIndex("")
		site, ipList := rtmDatRefs(ix)
		if site == nil && ipList == nil {
			t.Skip("в каталоге dat стенда нет подходящих списков")
		}
		// Оба ядра: один и тот же список должен попасть и в Xray (ext:), и в Mihomo.
		e := rtNewEnv(t, rtWithDeps(func(d *Deps) { d.Lists = ix }))
		e.writeUserProfile(t)
		e.addMyLinks(t, "vless_ws")

		direct, reject := Target{Kind: KindDirect}, Target{Kind: KindReject}
		save := func(name string, r Rule) {
			t.Helper()
			if _, _, err := e.svc.SaveRule(e.rev(t), r); err != nil {
				t.Fatalf("SaveRule %s: %v", name, err)
			}
		}
		var want []string
		if site != nil {
			cat, err := ix.Extract(site.File, site.Tag)
			if err != nil {
				t.Fatalf("Extract %s:%s: %v", site.File, site.Tag, err)
			}
			save("geosite", Rule{Enabled: true, Target: direct, Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{*site}}}})
			if len(cat.Domains) > 0 {
				want = append(want, "RULE-SET,"+ProviderName(site.File, site.Tag, listPartDomain)+",DIRECT")
			}
			if len(cat.Classical) > 0 {
				want = append(want, "RULE-SET,"+ProviderName(site.File, site.Tag, listPartClassical)+",DIRECT")
			}
		}
		if ipList != nil {
			cat, err := ix.Extract(ipList.File, ipList.Tag)
			if err != nil {
				t.Fatalf("Extract %s:%s: %v", ipList.File, ipList.Tag, err)
			}
			save("geoip", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondGeoip, Lists: []lists.ListRef{*ipList}}}})
			if len(cat.CIDRs) > 0 {
				want = append(want, "RULE-SET,"+ProviderName(ipList.File, ipList.Tag, listPartIP)+",REJECT,no-resolve")
			}
		}
		save("domain_port", Rule{Enabled: true, Target: direct, Conds: []Cond{
			{Type: CondDomain, Values: []string{"two.example.test"}}, {Type: CondPort, Values: []string{"8443"}},
		}})
		want = append(want, "AND,((DOMAIN-SUFFIX,two.example.test),(DST-PORT,8443)),DIRECT")
		save("bittorrent", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondBittorrent}}})
		save("raw_mihomo", Rule{Enabled: true, Target: direct, Raw: &RawRule{Kernel: configlayer.KernelMihomo, Text: "PROCESS-NAME,rt-raw-proc"}})
		want = append(want, "PROCESS-NAME,rt-raw-proc,DIRECT")
		if site != nil {
			// Тот же список во втором правиле: провайдер один.
			save("geosite_again", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{*site}}}})
			cat, _ := ix.Extract(site.File, site.Tag)
			if len(cat.Domains) > 0 {
				want = append(want, "RULE-SET,"+ProviderName(site.File, site.Tag, listPartDomain)+",REJECT")
			}
			if len(cat.Classical) > 0 {
				want = append(want, "RULE-SET,"+ProviderName(site.File, site.Tag, listPartClassical)+",REJECT")
			}
		}
		want = append(want, "MATCH,DIRECT")

		// «Применить»: mihomo -t читает профиль и файлы провайдеров, xray run -test —
		// ext:-списки из тех же dat.
		e.takeAndApply(t)

		doc := e.mihomoProfile(t)
		if got := rtmStrings(doc["rules"]); !rtxEqualStrings(got, want) {
			t.Errorf("rules профиля:\n got %q\nwant %q", got, want)
		}
		for _, line := range rtmStrings(doc["rules"]) {
			if strings.Contains(strings.ToLower(line), "bittorrent") {
				t.Errorf("правило «только Xray» попало в Mihomo: %q", line)
			}
		}

		// Файлы провайдеров: существуют, число строк равно Expect и числу записей
		// категории в dat (один источник данных для обоих ядер).
		gens := e.svc.Generators()
		files, err := gens[1].Generate(inputOf(e.state(t)), e.installed)
		if err != nil {
			t.Fatalf("генератор %s: %v", gens[1].ID(), err)
		}
		perList := map[string]int{}
		providers := 0
		for _, f := range files {
			if f.Kind != configlayer.KindMihomoRuleProviderText {
				continue
			}
			providers++
			if f.Inert || f.Expect == nil || f.Expect.ProviderType != "rules" {
				t.Errorf("провайдер %s: Inert=%v Expect=%+v", f.RelPath, f.Inert, f.Expect)
				continue
			}
			if !strings.HasPrefix(f.RelPath, "rule_providers/"+f.Expect.ProviderName+".") {
				t.Errorf("имя провайдера %q не совпадает с файлом %s", f.Expect.ProviderName, f.RelPath)
			}
			n := rtmCountLines(t, e.mihomoPath(f.RelPath))
			if n != f.Expect.Count {
				t.Errorf("в %s строк %d, Expect.Count %d", f.RelPath, n, f.Expect.Count)
			}
			// Имя «xcp-ls-<8hex>-<часть>»: список — всё до последнего дефиса.
			perList[f.Expect.ProviderName[:strings.LastIndex(f.Expect.ProviderName, "-")]] += n
		}
		if provs, _ := doc["rule-providers"].(map[string]any); len(provs) != providers {
			t.Errorf("rule-providers профиля %d, файлов провайдеров %d", len(provs), providers)
		}
		for _, ref := range []*lists.ListRef{site, ipList} {
			if ref == nil {
				continue
			}
			cat, err := ix.Extract(ref.File, ref.Tag)
			if err != nil {
				t.Fatal(err)
			}
			prefix := strings.TrimSuffix(ProviderName(ref.File, ref.Tag, listPartDomain), "-"+listPartDomain)
			if got := perList[prefix]; got != cat.Entries {
				t.Errorf("список %s:%s: в провайдерах %d строк, в категории dat %d", ref.File, ref.Tag, got, cat.Entries)
			}
		}

		// Тот же список в Xray — ext:<файл>:<тег>.
		xrules := rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile)))
		for _, c := range []struct {
			ref   *lists.ListRef
			field string
		}{{site, "domain"}, {ipList, "ip"}} {
			if c.ref == nil {
				continue
			}
			ext := "ext:" + c.ref.File + ":" + c.ref.Tag
			found := false
			for _, r := range xrules {
				if v := rtxStrings(r[c.field]); len(v) == 1 && v[0] == ext {
					found = true
				}
			}
			if !found {
				t.Errorf("в routing Xray нет правила с %s", ext)
			}
		}
	})

	t.Run("version_threshold", func(t *testing.T) {
		// Ниже Mihomo 1.19.27 группы не материализуются, цели групп — REJECT,
		// «Применить» проходит (D-45).
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithDeps(func(d *Deps) {
			d.KernelVersion = func(kernel string) string {
				if kernel == configlayer.KernelMihomo {
					return "1.19.26"
				}
				return ""
			}
		}))
		e.writeUserProfile(t)
		e.addMyLinks(t, "vless_ws", "trojan_grpc")
		auto, _ := rtGroupByName(e.groups(t), "Авто")
		if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
			Enabled: true, Target: Target{Kind: KindGroup, ID: auto.ID},
			Conds: []Cond{{Type: CondDomain, Values: []string{"group.example.test"}}},
		}); err != nil {
			t.Fatalf("SaveRule: %v", err)
		}
		if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: auto.ID}); err != nil {
			t.Fatalf("SetFinal: %v", err)
		}
		e.takeAndApply(t)

		doc := e.mihomoProfile(t)
		if _, has := doc["proxy-groups"]; has {
			t.Errorf("ниже 1.19.27 в профиле есть proxy-groups: %v", doc["proxy-groups"])
		}
		want := []string{"DOMAIN-SUFFIX,group.example.test,REJECT", "MATCH,REJECT"}
		if got := rtmStrings(doc["rules"]); !rtxEqualStrings(got, want) {
			t.Errorf("rules профиля %q, ожидались %q (цели групп — REJECT)", got, want)
		}
	})
}
