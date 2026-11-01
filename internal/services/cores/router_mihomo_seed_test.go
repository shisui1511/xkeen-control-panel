//go:build router

package cores

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Перенос профиля Mihomo по секциям (D-06, MIHO-01): настоящий разбор YAML,
// профиль панели собирается из частей и проходит настоящий `mihomo -t`
// устройства. Ядро стенда не трогается и не читается — метка ядра не нужна.

// rtMihomoSeedBin — бинарник Mihomo на устройстве.
const rtMihomoSeedBin = "/opt/sbin/mihomo"

// rtMihomoSeedTail — хвост маршрутизации, который в профиль панели дописывает
// генератор (здесь — минимальный).
const rtMihomoSeedTail = "proxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"

// rtMihomoSeedBasic — прежний профиль с ключами, которые раньше терялись.
const rtMihomoSeedBasic = "mode: rule\n" +
	"hosts:\n" +
	"  zz.example.test: 127.0.0.1\n" +
	"  aa.example.test: 127.0.0.2\n" +
	"  mm.example.test: 127.0.0.3\n" +
	"proxies: []\n" +
	"rules:\n" +
	"  - MATCH,DIRECT\n"

// rtMihomoSeedSections превращает результат переноса в секции черновика: под
// управлением только перечисленные идентификаторы.
func rtMihomoSeedSections(t *testing.T, seeded map[string]json.RawMessage, managed ...string) configlayer.Sections {
	t.Helper()
	out := configlayer.Sections{}
	for _, id := range managed {
		raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: true, Value: seeded[id]})
		if err != nil {
			t.Fatalf("секция %s: %v", id, err)
		}
		out[configlayer.CoreSectionName(configlayer.KernelMihomo, id)] = raw
	}
	return out
}

// rtMihomoSeedProfile собирает профиль панели из частей секций и хвоста
// маршрутизации так же, как генератор: содержимое частей идёт в корень.
func rtMihomoSeedProfile(t *testing.T, s configlayer.Sections, ports configlayer.MihomoPorts) string {
	t.Helper()
	parts, err := MihomoProfileParts(s, ports)
	if err != nil {
		t.Fatalf("MihomoProfileParts: %v", err)
	}
	if len(parts) != 5 {
		t.Fatalf("частей профиля %d, ждали 5", len(parts))
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, p := range parts {
		if p.Kind != yaml.MappingNode {
			t.Fatalf("часть профиля не отображение")
		}
		root.Content = append(root.Content, p.Content...)
	}
	var tail yaml.Node
	if err := yaml.Unmarshal([]byte(rtMihomoSeedTail), &tail); err != nil || len(tail.Content) == 0 {
		t.Fatalf("хвост маршрутизации: %v", err)
	}
	root.Content = append(root.Content, tail.Content[0].Content...)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		t.Fatalf("кодирование профиля: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("кодирование профиля: %v", err)
	}
	return buf.String()
}

// rtMihomoSeedKernelCheck отдаёт профиль ядру Mihomo устройства (`mihomo -t`);
// ядро не установлено — сценарий пропускается.
func rtMihomoSeedKernelCheck(t *testing.T, profile string) {
	t.Helper()
	if _, err := os.Stat(rtMihomoSeedBin); err != nil {
		t.Skipf("Mihomo не установлен на устройстве: %v", err)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, rtMihomoSeedBin, "-t", "-d", dir, "-f", cfg)
	if code != 0 {
		tail := strings.TrimSpace(stdout + " " + stderr)
		if len(tail) > 1500 {
			tail = tail[len(tail)-1500:]
		}
		t.Fatalf("mihomo -t отверг профиль панели: код %d: %s\nпрофиль:\n%s", code, tail, profile)
	}
}

// rtMihomoSeedObject разбирает значение секции как упорядоченный объект.
func rtMihomoSeedObject(t *testing.T, raw json.RawMessage) *configlayer.OrderedObject {
	t.Helper()
	o, err := configlayer.ParseOrderedObject(raw)
	if err != nil {
		t.Fatalf("значение секции не объект: %v: %s", err, raw)
	}
	return o
}

// rtMihomoSeedOrdered проверяет, что подстроки встречаются в тексте в заданном
// порядке.
func rtMihomoSeedOrdered(t *testing.T, what, text string, items ...string) {
	t.Helper()
	prev := -1
	for _, it := range items {
		i := strings.Index(text, it)
		if i < 0 {
			t.Fatalf("%s: нет %q в тексте:\n%s", what, it, text)
		}
		if i < prev {
			t.Fatalf("%s: порядок нарушен на %q:\n%s", what, it, text)
		}
		prev = i
	}
}

// rtMihomoSeedFull — прежний профиль со всеми видами ключей: якорь и ключ
// слияния, hosts, ntp, sub-rules, experimental, dns с nameserver-policy,
// listeners redir, tun, подключи profile, закреплённые routing-mark и сокет.
const rtMihomoSeedFull = "x-common: &c\n" +
	"  udp: true\n" +
	"mixed-port: 7890\n" +
	"allow-lan: false\n" +
	"mode: rule\n" +
	"log-level: info\n" +
	"routing-mark: 9\n" +
	"external-controller-unix: /tmp/x.sock\n" +
	"profile:\n" +
	"  store-selected: false\n" +
	"  store-fake-ip: true\n" +
	"hosts:\n" +
	"  zz.example.test: 127.0.0.1\n" +
	"  aa.example.test: 127.0.0.2\n" +
	"  mm.example.test: 127.0.0.3\n" +
	"ntp:\n" +
	"  enable: true\n" +
	"  server: time.example.test\n" +
	"sub-rules:\n" +
	"  SUB:\n" +
	"    - MATCH,DIRECT\n" +
	"experimental:\n" +
	"  quic-go-disable-ecn: true\n" +
	"dns:\n" +
	"  enable: true\n" +
	"  nameserver:\n" +
	"    - 127.0.0.1\n" +
	"  nameserver-policy:\n" +
	"    \"+.zz.example.test\": 127.0.0.1\n" +
	"    \"+.aa.example.test\": 127.0.0.2\n" +
	"    \"+.mm.example.test\": 127.0.0.3\n" +
	"listeners:\n" +
	"  - <<: *c\n" +
	"    name: in-redir\n" +
	"    type: redir\n" +
	"    port: 7100\n" +
	"  - name: force-proxy\n" +
	"    type: redir\n" +
	"    port: 7101\n" +
	"tun:\n" +
	"  enable: false\n" +
	"  stack: gvisor\n" +
	"proxies: []\n" +
	"proxy-groups: []\n" +
	"rules:\n" +
	"  - MATCH,DIRECT\n"

// rtMihomoSeedAll — все пять секций Mihomo под управлением.
var rtMihomoSeedAll = []string{"main", "dns", "inbounds", "sniffer", "other"}

func TestRouterCoresMihomoSeedRoundTrip(t *testing.T) {
	t.Run("hosts в Прочих ключах в прежнем порядке", func(t *testing.T) {
		seeded, err := SeedMihomo([]byte(rtMihomoSeedBasic), configlayer.MihomoPorts{})
		if err != nil {
			t.Fatalf("SeedMihomo: %v", err)
		}
		other := rtMihomoSeedObject(t, seeded["other"])
		if _, ok := other.Get("hosts"); !ok {
			t.Fatalf("hosts не перенесён в «Прочие ключи»: %s", seeded["other"])
		}
		for _, k := range []string{"proxies", "rules"} {
			if _, ok := other.Get(k); ok {
				t.Fatalf("ключ маршрутизации %s попал в «Прочие ключи»", k)
			}
		}
		profile := rtMihomoSeedProfile(t, rtMihomoSeedSections(t, seeded, "other"), configlayer.MihomoPorts{})
		rtMihomoSeedOrdered(t, "hosts в профиле панели", profile,
			"zz.example.test", "aa.example.test", "mm.example.test")
		rtMihomoSeedKernelCheck(t, profile)
	})
	t.Run("большой профиль: ключи маршрутизации не лимитируют перенос", func(t *testing.T) {
		// Правила и узлы подписки выводят текст за 256 КиБ и за 200 000 узлов,
		// но в секции не переносятся и бюджет ключей секций не тратят.
		var b strings.Builder
		b.WriteString(rtMihomoSeedBasic)
		b.WriteString("rule-providers:\n")
		for i := 0; b.Len() < 600<<10; i++ {
			fmt.Fprintf(&b, "  p%d: {type: http, behavior: domain, url: \"https://example.test/%d.yaml\"}\n", i, i)
		}
		seeded, err := SeedMihomo([]byte(b.String()), configlayer.MihomoPorts{})
		if err != nil {
			t.Fatalf("SeedMihomo большого профиля: %v", err)
		}
		other := rtMihomoSeedObject(t, seeded["other"])
		if _, ok := other.Get("hosts"); !ok {
			t.Fatalf("hosts не перенесён из большого профиля: %s", seeded["other"])
		}
		if _, ok := other.Get("rule-providers"); ok {
			t.Fatal("rule-providers попал в «Прочие ключи»")
		}
	})
	t.Run("все ключи по секциям, закреплённое поверх", func(t *testing.T) {
		seeded, err := SeedMihomo([]byte(rtMihomoSeedFull), configlayer.MihomoPorts{Redir: 7100, TProxy: 7101})
		if err != nil {
			t.Fatalf("SeedMihomo: %v", err)
		}
		mainObj := rtMihomoSeedObject(t, seeded["main"])
		for _, k := range []string{"redir-port", "tproxy-port", "routing-mark", "external-controller-unix"} {
			if _, ok := mainObj.Get(k); ok {
				t.Fatalf("в «Основном» не должно быть %s (listeners redir есть, закреплённое ведёт панель)", k)
			}
		}
		if v, _ := mainObj.Get("mixed-port"); string(v) != "7890" {
			t.Fatalf("mixed-port потерян: %s", seeded["main"])
		}
		prof, ok := mainObj.Get("profile")
		if !ok {
			t.Fatalf("подключи profile не перенесены в «Основное»: %s", seeded["main"])
		}
		po := rtMihomoSeedObject(t, prof)
		if _, ok := po.Get("store-selected"); ok {
			t.Fatalf("store-selected перенесён в «Основное»: %s", prof)
		}
		if v, _ := po.Get("store-fake-ip"); string(v) != "true" {
			t.Fatalf("store-fake-ip потерян: %s", prof)
		}

		other := rtMihomoSeedObject(t, seeded["other"])
		if got := strings.Join(other.Keys(), ","); got != "x-common,hosts,ntp,sub-rules,experimental" {
			t.Fatalf("«Прочие ключи»: %s, ждали x-common,hosts,ntp,sub-rules,experimental в порядке профиля", got)
		}

		sections := rtMihomoSeedSections(t, seeded, rtMihomoSeedAll...)
		profile := rtMihomoSeedProfile(t, sections, configlayer.MihomoPorts{Redir: 7100, TProxy: 7101})
		for _, want := range []string{"routing-mark: 255", "external-controller-unix: /opt/var/run/mihomo.sock", "store-selected: true", "store-fake-ip: true"} {
			if !strings.Contains(profile, want) {
				t.Fatalf("в профиле панели нет %q:\n%s", want, profile)
			}
		}
		for _, bad := range []string{"routing-mark: 9", "/tmp/x.sock", "store-selected: false"} {
			if strings.Contains(profile, bad) {
				t.Fatalf("в профиле панели осталось прежнее закреплённое %q:\n%s", bad, profile)
			}
		}
		rtMihomoSeedOrdered(t, "nameserver-policy", profile, "+.zz.example.test", "+.aa.example.test", "+.mm.example.test")
		rtMihomoSeedOrdered(t, "hosts", profile, "\n  zz.example.test: 127.0.0.1", "\n  aa.example.test: 127.0.0.2", "\n  mm.example.test: 127.0.0.3")

		// Ключ слияния раскрыт: listener in-redir получил udp из якоря.
		value, _, err := ParseYAMLText(profile)
		if err != nil {
			t.Fatalf("профиль панели не разбирается: %v", err)
		}
		top := rtMihomoSeedObject(t, value)
		lraw, _ := top.Get("listeners")
		var listeners []map[string]any
		if err := json.Unmarshal(lraw, &listeners); err != nil || len(listeners) != 2 {
			t.Fatalf("listeners профиля панели: %v: %s", err, lraw)
		}
		if listeners[0]["name"] != "in-redir" || listeners[0]["udp"] != true {
			t.Fatalf("listener in-redir не получил udp из якоря: %v", listeners[0])
		}
		if names := MihomoListenerNames(sections); strings.Join(names, ",") != "in-redir,force-proxy" {
			t.Fatalf("MihomoListenerNames: %v", names)
		}
		rtMihomoSeedKernelCheck(t, profile)
	})

	t.Run("нет прежнего профиля: все секции из пресетов", func(t *testing.T) {
		ports := configlayer.MihomoPorts{Redir: 7000, TProxy: 7001}
		seeded, err := SeedMihomo(nil, ports)
		if err != nil {
			t.Fatalf("SeedMihomo(nil): %v", err)
		}
		for _, id := range rtMihomoSeedAll {
			if string(seeded[id]) != string(MihomoPreset(id, ports)) {
				t.Fatalf("секция %s не равна пресету: %s", id, seeded[id])
			}
		}
		if v, _ := rtMihomoSeedObject(t, seeded["main"]).Get("redir-port"); string(v) != "7000" {
			t.Fatalf("redir-port пресета: %s", seeded["main"])
		}
		rtMihomoSeedKernelCheck(t, rtMihomoSeedProfile(t, rtMihomoSeedSections(t, seeded, rtMihomoSeedAll...), ports))
	})

	t.Run("состояние без секций: пять частей из пресетов", func(t *testing.T) {
		profile := rtMihomoSeedProfile(t, configlayer.Sections{}, configlayer.MihomoPorts{})
		for _, want := range []string{"redir-port: 5000", "tproxy-port: 5001", "routing-mark: 255", "store-selected: true", "sniffer:", "dns:"} {
			if !strings.Contains(profile, want) {
				t.Fatalf("в профиле из пресетов нет %q:\n%s", want, profile)
			}
		}
		rtMihomoSeedKernelCheck(t, profile)
	})
	t.Run("проверка", func(t *testing.T) {
		seeded, err := SeedMihomo([]byte(rtMihomoSeedFull), configlayer.MihomoPorts{Redir: 7100, TProxy: 7101})
		if err != nil {
			t.Fatalf("SeedMihomo: %v", err)
		}
		// issues возвращает коды проблем значения секции id.
		issues := func(id string, value json.RawMessage) []string {
			var out []string
			for _, is := range MihomoValueIssues(id, value) {
				out = append(out, is.Code+":"+is.Key)
			}
			return out
		}
		for _, id := range rtMihomoSeedAll {
			if got := issues(id, seeded[id]); len(got) != 0 {
				t.Fatalf("секция %s из переноса даёт проблемы: %v", id, got)
			}
		}
		// with возвращает значение секции с заменой одного ключа.
		with := func(id, key, val string) json.RawMessage {
			o := rtMihomoSeedObject(t, seeded[id])
			o.Set(key, json.RawMessage(val))
			b, err := o.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
		wantIssue := func(what string, got []string, want string) {
			t.Helper()
			for _, g := range got {
				if g == want {
					return
				}
			}
			t.Fatalf("%s: ждали %s, получили %v", what, want, got)
		}

		// API по TCP без secret закрыт, с secret — нет, профиль проходит mihomo -t.
		open := with("other", "external-controller", `"0.0.0.0:9090"`)
		wantIssue("external-controller без secret", issues("other", open), "controller_no_secret:external-controller")
		secured := with("other", "secret", `"s"`)
		o := rtMihomoSeedObject(t, secured)
		o.Set("external-controller", json.RawMessage(`"0.0.0.0:9090"`))
		securedRaw, err := o.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if got := issues("other", securedRaw); len(got) != 0 {
			t.Fatalf("external-controller с secret даёт проблемы: %v", got)
		}

		// CheckMihomo ссылается на секцию черновика и ключ, значения secret нет.
		in := configlayer.Input{Sections: rtMihomoSeedSections(t, map[string]json.RawMessage{"other": open}, "other")}
		bi := CheckMihomo(in, configlayer.InstalledKernels{Mihomo: true})
		if len(bi) != 1 || bi[0].Key != configlayer.SectionMihomoOther || bi[0].Code != configlayer.IssueControllerNoSecret ||
			bi[0].Params["key"] != "external-controller" {
			t.Fatalf("CheckMihomo: %+v", bi)
		}
		if got := CheckMihomo(in, configlayer.InstalledKernels{Xray: true}); len(got) != 0 {
			t.Fatalf("CheckMihomo без Mihomo: %+v", got)
		}

		wantIssue("rules в «Прочих ключах»", issues("other", with("other", "rules", `["MATCH,DIRECT"]`)), "raw_reserved:rules")
		wantIssue("routing-mark в «Основном»", issues("main", with("main", "routing-mark", "9")), "raw_pinned:routing-mark")
		wantIssue("порт вне диапазона", issues("main", with("main", "mixed-port", "70000")), "inbound_port_invalid:mixed-port")
		wantIssue("store-selected false", issues("main", with("main", "profile", `{"store-selected":false}`)), "raw_pinned:profile.store-selected")
		wantIssue("повтор имени listener", issues("inbounds",
			with("inbounds", "listeners", `[{"name":"in-redir","type":"redir","port":7100},{"name":"in-redir","type":"redir","port":7101}]`)),
			"inbound_duplicate:listeners[].name")

		// Профиль с защищённым API проходит ядро; проверка ядра — последней, она
		// пропускает сценарий, если Mihomo на устройстве нет.
		seeded["other"] = securedRaw
		rtMihomoSeedKernelCheck(t, rtMihomoSeedProfile(t, rtMihomoSeedSections(t, seeded, rtMihomoSeedAll...), configlayer.MihomoPorts{}))
	})
}
