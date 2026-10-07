package nodes

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestKey_RenameStable(t *testing.T) {
	n, err := ParseLink(readLink(t, "vless_reality"))
	if err != nil {
		t.Fatal(err)
	}
	src := "ab12cd34"
	key := n.Key(src)
	if key == "" || n.Identity() == "" {
		t.Fatal("ключ не вычислен")
	}
	if key != src+"|"+n.Identity() {
		t.Errorf("ключ %q не равен src|identity", key)
	}

	renamed := n
	renamed.Name = "Другое имя у провайдера"
	if renamed.Key(src) != key {
		t.Error("переименование изменило ключ")
	}
	if XrayTag(src, renamed.Key(src)) != XrayTag(src, key) {
		t.Error("переименование изменило тег Xray")
	}

	other := n
	other.UUID = "22222222-2222-4222-8222-222222222222"
	if other.Key(src) == key {
		t.Error("другой UUID дал тот же ключ")
	}
	if XrayTag(src, other.Key(src)) == XrayTag(src, key) {
		t.Error("другой UUID дал тот же тег")
	}
}

func TestHash8_RefFormat(t *testing.T) {
	h := Hash8("abc")
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(h) {
		t.Fatalf("Hash8 = %q", h)
	}
	// sha256("abc") = ba7816bf...
	if h != "ba7816bf" {
		t.Errorf("Hash8(abc) = %q, ожидалось ba7816bf", h)
	}
	if Ref("ab12cd34", "0a1b2c3d") != "ab12cd34/0a1b2c3d" {
		t.Errorf("Ref = %q", Ref("ab12cd34", "0a1b2c3d"))
	}
	if XrayTag("s", "k") != "xcp-s-"+Hash8("k") {
		t.Errorf("XrayTag = %q", XrayTag("s", "k"))
	}
}

func TestIdentity_Normalization(t *testing.T) {
	a := mustParse(t, "hysteria2")
	b, err := ParseLink("hy2://hy2-pass@node5.example:443#другое")
	if err != nil {
		t.Fatal(err)
	}
	if a.Identity() != b.Identity() {
		t.Errorf("hy2 и hysteria2 одного сервера: %q != %q", a.Identity(), b.Identity())
	}

	up := a
	up.Host = "NODE5.Example"
	if up.Identity() != a.Identity() {
		t.Error("регистр адреса изменил идентичность")
	}
	for _, alias := range []string{"hy2", "HYSTERIA2"} {
		x := a
		x.Protocol = alias
		if x.Identity() != a.Identity() {
			t.Errorf("протокол %q: идентичность изменилась", alias)
		}
	}
	ss := Node{Protocol: "ss", Host: "h.example", Port: 1, Password: "p"}
	ss2 := Node{Protocol: "shadowsocks", Host: "h.example", Port: 1, Password: "p"}
	if ss.Identity() != ss2.Identity() {
		t.Error("ss и shadowsocks должны совпадать")
	}

	other := a
	other.Password = "другой-пароль"
	if other.Identity() == a.Identity() {
		t.Error("другой пароль дал ту же идентичность")
	}
	if !strings.HasPrefix(a.Identity(), "hysteria2|node5.example|443|") {
		t.Errorf("формат идентичности: %q", a.Identity())
	}
}

func TestUniq_CollisionAndDuplicates(t *testing.T) {
	base := Node{Name: "A", Protocol: "vless", Host: "cdn.example", Port: 443, UUID: testUUID1,
		Transport: Transport{Network: "ws", Path: "/one"}}
	second := base
	second.Name = "B"
	second.Transport.Path = "/two"

	out, collapsed := Uniq("src1", []Node{base, second})
	if len(out) != 2 || collapsed != 0 {
		t.Fatalf("len=%d collapsed=%d", len(out), collapsed)
	}
	plain, suffixed := 0, 0
	for _, k := range out {
		if i := strings.Index(k.Key, "~"); i >= 0 {
			suffixed++
			if len(k.Key)-i-1 != 6 {
				t.Errorf("суффикс не из 6 hex: %q", k.Key)
			}
			if !regexp.MustCompile(`~[0-9a-f]{6}$`).MatchString(k.Key) {
				t.Errorf("суффикс: %q", k.Key)
			}
		} else {
			plain++
		}
	}
	if plain != 1 || suffixed != 1 {
		t.Errorf("plain=%d suffixed=%d", plain, suffixed)
	}
	if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].Key < out[j].Key }) {
		t.Error("результат не отсортирован по Key")
	}

	// детерминизм при перестановке входа: тот же путь получает тот же ключ
	rev, _ := Uniq("src1", []Node{second, base})
	byPath := func(list []Keyed) map[string]string {
		m := map[string]string{}
		for _, k := range list {
			m[k.Node.Transport.Path] = k.Key
		}
		return m
	}
	if !reflect.DeepEqual(byPath(out), byPath(rev)) {
		t.Errorf("перестановка входа изменила ключи: %v != %v", byPath(out), byPath(rev))
	}

	// полные дубли схлопываются
	dup := base
	dup.Name = "A-копия"
	out, collapsed = Uniq("src1", []Node{base, dup, second})
	if len(out) != 2 || collapsed != 1 {
		t.Errorf("дубли: len=%d collapsed=%d", len(out), collapsed)
	}
	out, collapsed = Uniq("src1", []Node{base, base})
	if len(out) != 1 || collapsed != 1 || strings.Contains(out[0].Key, "~") {
		t.Errorf("два одинаковых: %+v collapsed=%d", out, collapsed)
	}

	// разные узлы не мешают друг другу
	diff := base
	diff.UUID = testUUID2
	out, collapsed = Uniq("src1", []Node{base, diff})
	if len(out) != 2 || collapsed != 0 {
		t.Errorf("разные узлы: len=%d collapsed=%d", len(out), collapsed)
	}
	for _, k := range out {
		if strings.Contains(k.Key, "~") {
			t.Errorf("суффикс у узлов без коллизии: %q", k.Key)
		}
	}
	if out, c := Uniq("s", nil); len(out) != 0 || c != 0 {
		t.Errorf("пустой вход: %v %d", out, c)
	}
}
