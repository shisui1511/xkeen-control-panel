package nodes

import (
	"regexp"
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
