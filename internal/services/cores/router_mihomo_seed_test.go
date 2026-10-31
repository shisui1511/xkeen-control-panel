//go:build router

package cores

import (
	"bytes"
	"context"
	"encoding/json"
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
}
