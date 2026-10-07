package nodes

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// -update перезаписывает файлы testdata/golden/*.
var update = flag.Bool("update", false, "перезаписать золотые файлы testdata/golden")

// readLink читает ссылку-фикстуру testdata/links/<name>.txt (одна строка).
func readLink(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "links", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// checkGolden сверяет got с testdata/golden/<name> побайтно; с -update
// переписывает файл.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("золотой файл %s не найден (запустите с -update): %v", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("золотой файл %s не совпал\n--- ожидалось ---\n%s\n--- получено ---\n%s", name, want, got)
	}
}

// jsonMap приводит карту к виду, который получает потребитель файла.
func jsonMap(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTracer_VlessReality(t *testing.T) {
	line := readLink(t, "vless_reality")
	n, err := ParseLink(line)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if n.Protocol != "vless" || n.Origin != OriginLink || n.Format != "link" || n.Text != line {
		t.Fatalf("метаданные узла: %+v", n)
	}
	if n.Host != "node1.example" || n.Port != 443 || n.UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("адрес и учётные данные: %+v", n)
	}
	if n.Name != "Узел Reality" {
		t.Errorf("имя = %q", n.Name)
	}
	if n.Reality == nil || n.Reality.PublicKey == "" || n.Reality.ShortID != "0a1b2c3d" || n.Reality.SpiderX != "/" {
		t.Fatalf("reality: %+v", n.Reality)
	}
	if n.TLS == nil || len(n.TLS.ALPN) != 2 || n.TLS.Fingerprint != "chrome" || n.TLS.SNI != "cdn.example" {
		t.Fatalf("tls: %+v", n.TLS)
	}
	if n.PacketEncoding != "xudp" || n.Flow != "xtls-rprx-vision" || n.Encryption != "none" {
		t.Fatalf("flow/packet/encryption: %+v", n)
	}

	src := "ab12cd34"
	tag := XrayTag(src, n.Key(src))
	if !regexp.MustCompile(`^xcp-ab12cd34-[0-9a-f]{8}$`).MatchString(tag) {
		t.Fatalf("тег = %q", tag)
	}

	ob, err := XrayOutbound(n, tag)
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	data, err := XrayFile([]map[string]any{ob})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"mark": 255`)) {
		t.Errorf("в файле Xray нет sockopt.mark 255:\n%s", data)
	}
	checkGolden(t, "vless_reality.xray.json", data)

	px, err := ClashProxy(n, "Имя")
	if err != nil {
		t.Fatalf("ClashProxy: %v", err)
	}
	if _, bad := px["cipher"]; bad {
		t.Error("у vless не должно быть ключа cipher")
	}
	for _, k := range []string{"flow", "udp", "alpn", "packet-encoding", "reality-opts", "client-fingerprint", "servername"} {
		if _, ok := px[k]; !ok {
			t.Errorf("в прокси нет ключа %s: %v", k, px)
		}
	}
	if alpn, ok := px["alpn"].([]string); !ok || len(alpn) != 2 {
		t.Errorf("alpn должен быть списком из двух элементов: %#v", px["alpn"])
	}
	cdata, err := ClashFile([]map[string]any{px})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "vless_reality.clash.yaml", cdata)
}

func TestGolden_Deterministic(t *testing.T) {
	n, err := ParseLink(readLink(t, "vless_reality"))
	if err != nil {
		t.Fatal(err)
	}
	var firstX, firstC []byte
	for i := 0; i < 20; i++ {
		ob, err := XrayOutbound(n, "xcp-t-00000000")
		if err != nil {
			t.Fatal(err)
		}
		x, err := XrayFile([]map[string]any{ob})
		if err != nil {
			t.Fatal(err)
		}
		px, err := ClashProxy(n, "N")
		if err != nil {
			t.Fatal(err)
		}
		c, err := ClashFile([]map[string]any{px})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstX, firstC = x, c
			continue
		}
		if !bytes.Equal(x, firstX) || !bytes.Equal(c, firstC) {
			t.Fatalf("вывод недетерминирован на итерации %d", i)
		}
	}
	if len(firstX) == 0 || len(firstC) == 0 {
		t.Fatal("пустой вывод")
	}
}
