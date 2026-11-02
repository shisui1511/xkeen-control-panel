//go:build router

package routing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Профиль Mihomo панели из секций и перенос при взятии маршрутизации: настоящий
// слой, настоящий `mihomo -t` устройства в конвейере «Применить» (профиль
// панели проверяется как config.yaml), файлы читаются с диска. Ядро стенда не
// трогается (Applier не задан), ядро тест не читает и не переключает — метка
// ядра не нужна.

// rtmsFixture — прежний активный профиль со всеми видами ключей: hosts, ntp,
// sub-rules, experimental, dns с nameserver-policy в «неалфавитном» порядке,
// подключи profile, входящий redir и tun. Порядок ключей в hosts и
// nameserver-policy намеренно не алфавитный.
const rtmsFixture = "mode: rule\n" +
	"log-level: info\n" +
	"allow-lan: true\n" +
	"profile:\n" +
	"  store-fake-ip: true\n" +
	"hosts:\n" +
	"  zz.example.test: 127.0.0.1\n" +
	"  aa.example.test: 127.0.0.2\n" +
	"  mm.example.test: 127.0.0.3\n" +
	"ntp:\n" +
	"  enable: false\n" +
	"  server: ntp.example.test\n" +
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
	"  - name: in-redir\n" +
	"    type: redir\n" +
	"    port: 7100\n" +
	"tun:\n" +
	"  enable: false\n" +
	"  stack: gvisor\n" +
	"proxies: []\n" +
	"proxy-groups: []\n" +
	"rules:\n" +
	"  - MATCH,DIRECT\n"

// rtmsWriteProfile кладёт прежний профиль profiles/user.yaml и симлинк
// config.yaml на него; возвращает путь профиля.
func rtmsWriteProfile(t *testing.T, e *rtEnv, content string) string {
	t.Helper()
	dir := filepath.Join(e.mihomoDir(), "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(dir, "user.yaml")
	if err := os.WriteFile(user, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(user, filepath.Join(e.mihomoDir(), "config.yaml")); err != nil {
		t.Fatal(err)
	}
	return user
}

// rtmsSection читает секцию Mihomo черновика (или применённого состояния).
func rtmsSection(t *testing.T, sections configlayer.Sections, id string) configlayer.CoreSection {
	t.Helper()
	name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
	cs, err := configlayer.ParseCoreSection(sections[name])
	if err != nil {
		t.Fatalf("секция %s: %v", name, err)
	}
	return cs
}

// rtmsNode читает профиль панели как дерево YAML (порядок ключей сохранён).
func rtmsNode(t *testing.T, e *rtEnv) *yaml.Node {
	t.Helper()
	data, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		t.Fatalf("профиль панели: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		t.Fatalf("профиль панели не YAML: %v", err)
	}
	return doc.Content[0]
}

// rtmsKeys возвращает ключи отображения YAML в порядке файла.
func rtmsKeys(n *yaml.Node) []string {
	var keys []string
	if n == nil || n.Kind != yaml.MappingNode {
		return keys
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

// rtmsChild возвращает значение ключа отображения YAML (nil — ключа нет).
func rtmsChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// rtmsIndex возвращает позицию ключа в списке; нет ключа — провал теста.
func rtmsIndex(t *testing.T, keys []string, key string) int {
	t.Helper()
	i := slices.Index(keys, key)
	if i < 0 {
		t.Fatalf("в профиле панели нет ключа %q: %v", key, keys)
	}
	return i
}

// rtmsPanelBase проверяет закреплённое в профиле панели: метка сокетов, сокет
// контроллера и сохранение выбора групп.
func rtmsPanelBase(t *testing.T, doc map[string]any) {
	t.Helper()
	if doc["routing-mark"] != 255 {
		t.Errorf("routing-mark %v, ожидалось 255", doc["routing-mark"])
	}
	if doc["external-controller-unix"] != services.DefaultMihomoSocketPath {
		t.Errorf("external-controller-unix %v, ожидался сокет панели", doc["external-controller-unix"])
	}
	if prof, _ := doc["profile"].(map[string]any); prof["store-selected"] != true {
		t.Errorf("profile.store-selected: %v", doc["profile"])
	}
}

// rtmsRemoveSections удаляет из черновика все секции mihomo_*.
func rtmsRemoveSections(t *testing.T, e *rtEnv) {
	t.Helper()
	drop := map[string]json.RawMessage{}
	for _, id := range configlayer.MihomoSectionIDs {
		drop[configlayer.CoreSectionName(configlayer.KernelMihomo, id)] = nil
	}
	if _, err := e.layer.EditDraftBatch(e.rev(t), drop, nil); err != nil {
		t.Fatalf("удаление секций mihomo_*: %v", err)
	}
}

// «Управлять в панели» переносит в секции Mihomo все ключи прежнего профиля, а
// профиль панели их сохраняет в прежнем порядке; «Отпустить» возвращает прежний
// профиль, а значения секций остаются сохранёнными и возвращаются повторным
// взятием (D-09); профиль без секций собирается из пресетов с портами
// прежнего профиля; без прежнего профиля секции берутся из встроенной базы.
func TestRouterMihomoSectionsKeepUserKeys(t *testing.T) {
	t.Run("take_keeps_user_keys", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		user := rtmsWriteProfile(t, e, rtmsFixture)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}

		// Одна правка черновика: желание взять и пять секций с from — имя и md5
		// прежнего профиля.
		st := e.state(t)
		if !configlayer.DesiredRouting(st.Draft).Managed {
			t.Error("после Take routing.managed не взведён")
		}
		wantFrom := []configlayer.SourceFile{{Name: "profiles/user.yaml", Hash: configlayer.HashContent([]byte(rtmsFixture))}}
		for _, id := range configlayer.MihomoSectionIDs {
			cs := rtmsSection(t, st.Draft, id)
			if !cs.Managed || cs.Preset || !reflect.DeepEqual(cs.From, wantFrom) {
				t.Errorf("секция mihomo_%s после Take: managed=%v preset=%v from=%v, ожидались managed, не пресет, from %v",
					id, cs.Managed, cs.Preset, cs.From, wantFrom)
			}
		}

		// «Применить»: проверка mihomo -t шла внутри конвейера (результат applied).
		e.applyOK(t)
		if got, err := filepath.EvalSymlinks(e.mihomoPath("config.yaml")); err != nil || got != mustEval(t, e.mihomoPath(configlayer.PanelMihomoProfile)) {
			t.Errorf("config.yaml указывает на %q (%v), ожидался профиль панели", got, err)
		}

		root := rtmsNode(t, e)
		keys := rtmsKeys(root)
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				t.Errorf("ключ верхнего уровня %q повторён в профиле панели", k)
			}
			seen[k] = true
		}
		// Ключи пользователя на месте и в прежнем порядке.
		hosts, ntp, sub, exp := rtmsIndex(t, keys, "hosts"), rtmsIndex(t, keys, "ntp"), rtmsIndex(t, keys, "sub-rules"), rtmsIndex(t, keys, "experimental")
		if !(hosts < ntp && ntp < sub && sub < exp) {
			t.Errorf("порядок прочих ключей нарушен: %v", keys)
		}
		if got := rtmsKeys(rtmsChild(root, "hosts")); !rtxEqualStrings(got, []string{"zz.example.test", "aa.example.test", "mm.example.test"}) {
			t.Errorf("порядок hosts %v, ожидался исходный", got)
		}
		dns := rtmsChild(root, "dns")
		if got := rtmsKeys(rtmsChild(dns, "nameserver-policy")); !rtxEqualStrings(got, []string{"+.zz.example.test", "+.aa.example.test", "+.mm.example.test"}) {
			t.Errorf("порядок nameserver-policy %v, ожидался исходный", got)
		}

		var doc map[string]any
		if err := rtmsDecode(e, &doc); err != nil {
			t.Fatal(err)
		}
		rtmsPanelBase(t, doc)
		if prof, _ := doc["profile"].(map[string]any); prof["store-fake-ip"] != true {
			t.Errorf("подключ profile.store-fake-ip потерян: %v", doc["profile"])
		}
		if ntp, _ := doc["ntp"].(map[string]any); ntp["server"] != "ntp.example.test" || ntp["enable"] != false {
			t.Errorf("ntp: %v", doc["ntp"])
		}
		if exp, _ := doc["experimental"].(map[string]any); exp["quic-go-disable-ecn"] != true {
			t.Errorf("experimental: %v", doc["experimental"])
		}
		if sr, _ := doc["sub-rules"].(map[string]any); sr["SUB"] == nil {
			t.Errorf("sub-rules: %v", doc["sub-rules"])
		}
		if tun, _ := doc["tun"].(map[string]any); tun["stack"] != "gvisor" || tun["enable"] != false {
			t.Errorf("tun: %v", doc["tun"])
		}
		ls, _ := doc["listeners"].([]any)
		if l0, _ := firstMap(ls); l0["name"] != "in-redir" || l0["type"] != "redir" || l0["port"] != 7100 {
			t.Errorf("listeners: %v", doc["listeners"])
		}
		if doc["log-level"] != "info" || doc["mode"] != "rule" {
			t.Errorf("основное: log-level=%v mode=%v", doc["log-level"], doc["mode"])
		}
		// Прежний профиль на месте и не изменился.
		if data, err := os.ReadFile(user); err != nil || string(data) != rtmsFixture {
			t.Errorf("прежний профиль изменён: %v", err)
		}
	})

	t.Run("release_restores_profile_and_keeps_values", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		user := rtmsWriteProfile(t, e, rtmsFixture)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		e.applyOK(t)

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, e.state(t).Draft, id); cs.Managed || len(cs.Value) == 0 || len(cs.From) != 0 {
				t.Errorf("секция mihomo_%s в черновике после Release: managed=%v value=%d from=%v, ожидались managed=false, значение сохранено, from пуст",
					id, cs.Managed, len(cs.Value), cs.From)
			}
		}
		e.applyOK(t)

		if link, err := os.Readlink(e.mihomoPath("config.yaml")); err != nil || link != user {
			t.Errorf("config.yaml после возврата: %q, %v (ожидался симлинк на прежний профиль)", link, err)
		}
		if data, err := os.ReadFile(user); err != nil || string(data) != rtmsFixture {
			t.Errorf("прежний профиль после возврата изменён побайтно: %v", err)
		}
		if _, err := os.Stat(e.mihomoPath(configlayer.PanelMihomoProfile)); err == nil {
			t.Error("после возврата профиль панели остался на диске")
		}
		st := e.state(t)
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, st.Applied, id); cs.Managed {
				t.Errorf("секция mihomo_%s в применённом состоянии остаётся под управлением", id)
			}
		}
	})

	// Отпустить и снова взять до «Применить»: секции возвращаются из применённого
	// состояния, ключи профиля панели (hosts, ntp, sub-rules) не теряются.
	t.Run("release_then_retake_before_apply_keeps_keys", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		rtmsWriteProfile(t, e, rtmsFixture)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		e.applyOK(t)

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("повторный Take: %v", err)
		}
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, e.state(t).Draft, id); !cs.Managed || cs.Preset {
				t.Errorf("секция mihomo_%s после повторного Take: managed=%v preset=%v, ожидалась возвращённая из применённого состояния", id, cs.Managed, cs.Preset)
			}
		}
		e.applyPasses(t)

		keys := rtmsKeys(rtmsNode(t, e))
		for _, k := range []string{"hosts", "ntp", "sub-rules", "experimental"} {
			rtmsIndex(t, keys, k)
		}
	})

	// D-09: значения панели переживают отпускание и возвращаются при взятии, а не
	// заменяются ключами прежнего профиля: перенос D-06 идёт только при первом
	// взятии.
	t.Run("retake_restores_saved_values", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		user := rtmsWriteProfile(t, e, rtmsFixture)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		// Значение панели в «Прочих ключах» (hosts), которого в прежнем профиле нет.
		other := configlayer.CoreSectionName(configlayer.KernelMihomo, "other")
		oc := rtmsSection(t, e.state(t).Draft, "other")
		if !bytes.Contains(oc.Value, []byte("zz.example.test")) {
			t.Fatalf("в секции other нет hosts прежнего профиля")
		}
		oc.Value = bytes.Replace(oc.Value, []byte("zz.example.test"), []byte("panel.example.test"), 1)
		raw, err := configlayer.EncodeCoreSection(oc)
		if err != nil {
			t.Fatal(err)
		}
		e.putSection(t, other, raw)
		e.applyOK(t)

		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		e.applyOK(t)

		// Прежний профиль изменили вне панели: другие hosts.
		changed := strings.Replace(rtmsFixture, "zz.example.test: 127.0.0.1", "other.example.test: 127.0.0.7", 1)
		if err := os.WriteFile(user, []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("повторный Take: %v", err)
		}
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, e.state(t).Draft, id); !cs.Managed || len(cs.From) != 0 {
				t.Errorf("секция mihomo_%s после повторного Take: managed=%v from=%v, ожидались managed и пустой from", id, cs.Managed, cs.From)
			}
		}
		e.applyOK(t)

		var doc map[string]any
		if err := rtmsDecode(e, &doc); err != nil {
			t.Fatal(err)
		}
		hostsDoc, _ := doc["hosts"].(map[string]any)
		if hostsDoc["panel.example.test"] != "127.0.0.1" {
			t.Errorf("hosts после повторного Take: ключи %v, ожидалось значение панели", rtmsKeys(rtmsChild(rtmsNode(t, e), "hosts")))
		}
		if _, ok := hostsDoc["other.example.test"]; ok {
			t.Error("hosts: ключ изменённого прежнего профиля перенесён при повторном Take")
		}
	})

	// D-09: сохранённые значения переживают «Сбросить» после применённого отпускания.
	t.Run("reset_keeps_saved_values", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		rtmsWriteProfile(t, e, rtmsFixture)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		e.applyOK(t)
		if _, err := e.svc.Release(e.rev(t)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		e.applyOK(t)

		if _, err := e.layer.ResetDraft(e.rev(t)); err != nil {
			t.Fatalf("ResetDraft: %v", err)
		}
		st := e.state(t)
		for _, id := range configlayer.MihomoSectionIDs {
			if cs := rtmsSection(t, st.Draft, id); cs.Managed || len(cs.Value) == 0 {
				t.Errorf("секция mihomo_%s после сброса: managed=%v value=%d, ожидались managed=false и значение", id, cs.Managed, len(cs.Value))
			}
			if cs := rtmsSection(t, st.Applied, id); cs.Managed || len(cs.Value) == 0 {
				t.Errorf("секция mihomo_%s в применённом состоянии: managed=%v value=%d", id, cs.Managed, len(cs.Value))
			}
		}
	})

	// Профиль больше 600 КиБ (объём в proxies и rules) берётся целиком: ключи
	// пользователя переносятся, маршрутизация прежнего профиля — нет (D-06). Прежний
	// предел разбора был 256 КиБ.
	t.Run("large_profile_take_keeps_keys", func(t *testing.T) {
		head, _, ok := strings.Cut(rtmsFixture, "proxies:")
		if !ok {
			t.Fatal("в rtmsFixture нет строки proxies:")
		}
		const minSize = 600 << 10
		var proxies, rules strings.Builder
		proxies.WriteString("proxies:\n")
		rules.WriteString("proxy-groups: []\nrules:\n")
		const tail = "  - MATCH,DIRECT\n"
		for i := 1; len(head)+proxies.Len()+rules.Len()+len(tail) < minSize; i++ {
			fmt.Fprintf(&proxies, "  - name: p%d\n    type: ss\n    server: p%d.example.test\n    port: %d\n    cipher: aes-128-gcm\n    password: stub\n",
				i, i, 20000+i%10000)
			fmt.Fprintf(&rules, "  - DOMAIN-SUFFIX,r%d.example.test,DIRECT\n", i)
		}
		profile := head + proxies.String() + rules.String() + tail
		if len(profile) < minSize {
			t.Fatalf("фикстура %d байт, нужно не меньше %d: разрыв не проверяется", len(profile), minSize)
		}

		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		rtmsWriteProfile(t, e, profile)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take большого профиля (%d КиБ): %v", len(profile)>>10, err)
		}

		st := e.state(t)
		for _, id := range configlayer.MihomoSectionIDs {
			cs := rtmsSection(t, st.Draft, id)
			if !cs.Managed || len(cs.From) == 0 || cs.From[0].Name != "profiles/user.yaml" {
				t.Errorf("секция mihomo_%s после Take большого профиля: managed=%v from=%v, ожидались managed и имя прежнего профиля", id, cs.Managed, cs.From)
			}
		}
		var other map[string]json.RawMessage
		if err := json.Unmarshal(rtmsSection(t, st.Draft, "other").Value, &other); err != nil {
			t.Fatalf("значение секции other не объект: %v", err)
		}
		for _, k := range []string{"proxies", "proxy-groups", "rules", "rule-providers"} {
			if _, found := other[k]; found {
				t.Errorf("в «Прочих ключах» ключ маршрутизации %q: маршрутизация прежнего профиля не переносится", k)
			}
		}

		e.applyOK(t)
		keys := rtmsKeys(rtmsNode(t, e))
		for _, k := range []string{"hosts", "ntp", "sub-rules", "experimental"} {
			rtmsIndex(t, keys, k)
		}
		text, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
		if err != nil {
			t.Fatalf("профиль панели: %v", err)
		}
		for _, leak := range []string{"p1.example.test", "r1.example.test"} {
			if bytes.Contains(text, []byte(leak)) {
				t.Errorf("в профиле панели остался элемент маршрутизации прежнего профиля (%s)", leak)
			}
		}
	})

	t.Run("no_sections_use_presets", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		e.writeUserProfile(t) // порты прежнего профиля 7000 и 7001
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		e.applyOK(t)

		// Состояние после 146: маршрутизация взята, секций Mihomo нет.
		rtmsRemoveSections(t, e)
		e.applyOK(t)

		var doc map[string]any
		if err := rtmsDecode(e, &doc); err != nil {
			t.Fatal(err)
		}
		rtmsPanelBase(t, doc)
		if doc["redir-port"] != 7000 || doc["tproxy-port"] != 7001 {
			t.Errorf("порты профиля %v/%v, ожидались 7000/7001 прежнего профиля", doc["redir-port"], doc["tproxy-port"])
		}
		if sn, _ := doc["sniffer"].(map[string]any); sn["enable"] != true || sn["override-destination"] != false {
			t.Errorf("sniffer пресета: %v", doc["sniffer"])
		}
		if dns, _ := doc["dns"].(map[string]any); dns["enable"] != true || dns["ipv6"] != false {
			t.Errorf("dns пресета: %v", doc["dns"])
		}

		// Два вызова генератора дают одинаковые байты.
		in := inputOf(e.state(t))
		first, err := mihomoGen{s: e.svc}.Generate(in, e.installed)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		second, err := mihomoGen{s: e.svc}.Generate(in, e.installed)
		if err != nil {
			t.Fatalf("Generate (повторно): %v", err)
		}
		if len(first) == 0 || len(first) != len(second) {
			t.Fatalf("файлов генератора %d и %d", len(first), len(second))
		}
		for i := range first {
			if first[i].RelPath != second[i].RelPath || !bytes.Equal(first[i].Content, second[i].Content) {
				t.Errorf("файл %s различается между вызовами генератора", first[i].RelPath)
			}
		}
	})

	t.Run("no_previous_profile_uses_presets", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		// Корень Mihomo без config.yaml: прежнего профиля нет.
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		st := e.state(t)
		for _, id := range configlayer.MihomoSectionIDs {
			cs := rtmsSection(t, st.Draft, id)
			if !cs.Managed || !cs.Preset || len(cs.From) != 0 || len(cs.Value) == 0 {
				t.Errorf("секция mihomo_%s без прежнего профиля: %+v, ожидались managed, preset, без from, со значением", id, cs)
			}
		}
		e.applyOK(t)

		var doc map[string]any
		if err := rtmsDecode(e, &doc); err != nil {
			t.Fatal(err)
		}
		rtmsPanelBase(t, doc)
		if doc["redir-port"] != 5000 || doc["tproxy-port"] != 5001 {
			t.Errorf("порты профиля %v/%v, ожидались 5000/5001 базы", doc["redir-port"], doc["tproxy-port"])
		}
	})
}

// mustEval возвращает путь после раскрытия симлинков; ошибка — провал теста.
func mustEval(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("путь %s: %v", path, err)
	}
	return p
}

// rtmsDecode читает профиль панели окружения как YAML-объект.
func rtmsDecode(e *rtEnv, out *map[string]any) error {
	data, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}

// firstMap возвращает первый элемент списка YAML как объект.
func firstMap(list []any) (map[string]any, bool) {
	if len(list) == 0 {
		return nil, false
	}
	m, ok := list[0].(map[string]any)
	return m, ok
}

// rtmsForeignRouting — чужой файл с правилом routing: «чистый», при взятии
// уходит в резервный набор.
const rtmsForeignRouting = `{
  "routing": {"rules": [{"type": "field", "domain": ["domain:extra.example.test"], "outboundTag": "` + TagDirect + `"}]}
}
`

// rtmsForeignBurst — чужой файл наблюдения: pingConfig с адресами проб,
// интервалом, таймаутом, sampling и ключом, для которого в форме поля нет.
const rtmsForeignBurst = `{
  "burstObservatory": {
    "subjectSelector": ["x"],
    "pingConfig": {
      "destination": "http://127.0.0.1:1/generate_204",
      "connectivity": "http://127.0.0.1:2/generate_204",
      "interval": "30s",
      "timeout": "4s",
      "sampling": 5,
      "httpMethod": "GET"
    }
  }
}
`

// rtmsForeignEnv создаёт окружение Xray с чужими 05_routing.json и 07_obs.json
// и узлами «Моих узлов».
func rtmsForeignEnv(t *testing.T) *rtEnv {
	t.Helper()
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	for name, body := range map[string]string{"05_routing.json": rtmsForeignRouting, "07_obs.json": rtmsForeignBurst} {
		if err := os.WriteFile(filepath.Join(e.xrayDir(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
	return e
}

// При взятии маршрутизации Xray настройки проб заполняются из чужого
// burstObservatory (адрес, интервал и таймаут в секундах, connectivity,
// sampling, остальные ключи pingConfig); пробы, которые пользователь уже менял,
// не перезаписываются.
func TestRouterXrayProbeSeedFromForeign(t *testing.T) {
	t.Run("seeded_from_foreign", func(t *testing.T) {
		e := rmsSeedEnv(t)
		wantSel := rtxXrayTags(e, t)

		got, err := e.svc.Probe()
		if err != nil {
			t.Fatal(err)
		}
		if got.URL != "http://127.0.0.1:1/generate_204" || got.IntervalS != 30 || got.TimeoutS != 4 ||
			got.Connectivity != "http://127.0.0.1:2/generate_204" || got.Sampling != 5 || string(got.Extra) != `{"httpMethod":"GET"}` {
			t.Fatalf("пробы после Take: %+v", got)
		}

		e.applyOK(t)
		ping, sub := rtpbPing(t, e)
		if ping["destination"] != got.URL || ping["connectivity"] != got.Connectivity ||
			ping["interval"] != "30s" || ping["timeout"] != "4s" || ping["sampling"] != float64(5) || ping["httpMethod"] != "GET" {
			t.Errorf("pingConfig файла панели: %v", ping)
		}
		if !rtxEqualStrings(sub, wantSel) {
			t.Errorf("subjectSelector %v, ожидались теги группы %v", sub, wantSel)
		}
	})

	t.Run("user_probe_kept", func(t *testing.T) {
		e := rtmsForeignEnv(t)
		saved := ProbeConfig{URL: rtxDefaultProbeURL, IntervalS: 60, TimeoutS: 5}
		if _, err := e.svc.SaveProbe(e.rev(t), saved); err != nil {
			t.Fatalf("SaveProbe: %v", err)
		}
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		want := saved
		want.V = sectionVersion
		if got, err := e.svc.Probe(); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("пробы пользователя после Take: %+v, %v, ожидались прежние %+v", got, err, want)
		}
	})
}

// rmsSeedEnv — окружение с чужими файлами и уже выполненным Take.
func rmsSeedEnv(t *testing.T) *rtEnv {
	t.Helper()
	e := rtmsForeignEnv(t)
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	return e
}
