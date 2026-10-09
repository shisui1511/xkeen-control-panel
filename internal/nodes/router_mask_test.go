//go:build router

package nodes

import (
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка заглушек и маскирования на устройстве: утверждения о свойствах
// функций на данных проверки (ссылки testdata/links), без ядер и без
// эталонных файлов.

func TestRouterStubs(t *testing.T) {
	routertest.Current(t)
	base := rtCapLink(t, "vless_ws")
	if IsStub(base) {
		t.Fatalf("рабочий узел распознан заглушкой")
	}
	mutate := func(f func(n *Node)) Node {
		n := base
		f(&n)
		return n
	}
	cases := []struct {
		name string
		node Node
		stub bool
	}{
		{"empty-host", mutate(func(n *Node) { n.Host = "" }), true},
		{"unspecified-host", mutate(func(n *Node) { n.Host = "0.0.0.0" }), true},
		{"loopback-host", mutate(func(n *Node) { n.Host = "127.0.0.1" }), true},
		{"port-0", mutate(func(n *Node) { n.Port = 0 }), true},
		{"port-1", mutate(func(n *Node) { n.Port = 1 }), true},
		{"port-2", mutate(func(n *Node) { n.Port = 2 }), false},
		{"zero-uuid", mutate(func(n *Node) { n.UUID = "00000000-0000-0000-0000-000000000000" }), true},
		{"zero-uuid-password", mutate(func(n *Node) { n.UUID, n.Password = "", "00000000-0000-0000-0000-000000000000" }), true},
		{"talking-name", mutate(func(n *Node) { n.Name = "Истекла подписка" }), false},
	}
	for _, c := range cases {
		if got := IsStub(c.node); got != c.stub {
			t.Errorf("%s: IsStub = %v, ожидалось %v", c.name, got, c.stub)
		}
	}

	list := []Node{base, cases[0].node, base, cases[4].node, base}
	list[2].Port = 8443
	working, stubs := SplitStubs(list)
	if len(working) != 3 || stubs != 2 {
		t.Fatalf("SplitStubs: рабочих %d, заглушек %d (ожидалось 3 и 2)", len(working), stubs)
	}
	if working[1].Port != 8443 {
		t.Errorf("SplitStubs изменил порядок рабочих узлов")
	}
}

func TestRouterMask(t *testing.T) {
	routertest.Current(t)

	urls := []struct{ in, want string }{
		{"https://sub.example/api/sub/AbCdEf123456?token=zzz", "https://sub.example/…/AbCd…"},
		{"https://user:pass@sub.example/api/sub/AbCdEf123456", "https://sub.example/…/AbCd…"},
		{"https://sub.example", "https://sub.example/…"},
		{"https://sub.example/", "https://sub.example/…"},
		{"https://sub.example/tok", "https://sub.example/…/…"},
		{"not a url", "…"},
		{"", "…"},
	}
	for _, c := range urls {
		got := MaskURL(c.in)
		if got != c.want {
			t.Errorf("MaskURL(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
		for _, leak := range []string{"token=", "zzz", "user", "pass@", "123456"} {
			if strings.Contains(got, leak) {
				t.Errorf("MaskURL(%q) оставил %q", c.in, leak)
			}
		}
	}

	for in, want := range map[string]string{"abcdef": "abcd…", "abcde": "abcd…", "abcd": "…", "abc": "…", "": ""} {
		if got := MaskSecret(in); got != want {
			t.Errorf("MaskSecret(%q) = %q, ожидалось %q", in, got, want)
		}
	}

	// Секреты узлов из данных проверки.
	vless := rtCapLink(t, "vless_reality")
	trojan := rtCapLink(t, "trojan_grpc")
	wg := rtCapLink(t, "wireguard")
	hy2 := rtCapLink(t, "hysteria2")
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}
	if s := Secrets(vless); !has(s, vless.UUID) || !has(s, vless.Reality.ShortID) {
		t.Errorf("Secrets(vless) без UUID или shortId: %d значений", len(s))
	}
	if s := Secrets(trojan); !has(s, trojan.Password) || len(s) != 1 {
		t.Errorf("Secrets(trojan): ожидался один пароль, получено %d значений", len(s))
	}
	if s := Secrets(wg); !has(s, wg.WG.PrivateKey) || wg.WG.PreSharedKey != "" && !has(s, wg.WG.PreSharedKey) {
		t.Errorf("Secrets(wireguard) без ключей: %d значений", len(s))
	}
	if s := Secrets(hy2); !has(s, hy2.Password) || hy2.Hy2.ObfsPassword == "" || !has(s, hy2.Hy2.ObfsPassword) {
		t.Errorf("Secrets(hysteria2) без пароля или пароля обфускации: %d значений", len(s))
	}
	ss := rtCapLink(t, "ss_plugin")
	ss.PluginOpts = map[string]any{"password": "plugin-secret-1", "host": "cdn.example", "psk": "plugin-psk-2"}
	if s := Secrets(ss); !has(s, "plugin-secret-1") || !has(s, "plugin-psk-2") || has(s, "cdn.example") {
		t.Errorf("Secrets(ss) параметры плагина: %v", len(s))
	}

	// Текст диагностики: ни один секрет, токен и путь подписки не переживает маску.
	secrets := append(append(Secrets(vless), Secrets(trojan)...), "tiny")
	text := "tiny uuid " + vless.UUID + " shortId " + vless.Reality.ShortID + " пароль " + trojan.Password +
		" источник https://sub.example/api/sub/SecretPathToken9?token=abcdef123 (ответ 500), password=hunter22secret"
	got := MaskText(text, secrets)
	for _, leak := range []string{vless.UUID, trojan.Password, "SecretPathToken9", "abcdef123", "hunter22secret", "/api/sub/"} {
		if strings.Contains(got, leak) {
			t.Errorf("MaskText оставил секрет %q: %s", MaskSecret(leak), got)
		}
	}
	if !strings.Contains(got, "https://sub.example/…/Secr…") || !strings.Contains(got, "(ответ 500)") {
		t.Errorf("MaskText повредил адрес или окружающий текст: %s", got)
	}
	if !strings.Contains(got, "tiny") {
		t.Errorf("MaskText заменил секрет короче шести символов: %s", got)
	}
}
