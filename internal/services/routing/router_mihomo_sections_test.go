//go:build router

package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
// профиль и снимает секции; профиль без секций собирается из пресетов с портами
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
