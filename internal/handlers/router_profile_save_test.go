//go:build router

package handlers

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Сохранение файла профиля Mihomo на настоящей панели устройства (PART-02):
// `mihomo -t` идёт в зеркале корня Mihomo, поэтому геобазы и каталоги провайдеров
// видны проверке, а отказ откатывает файл. Вердикт — коды ответов панели и байты
// файла на диске. Файлы rt-save* и rt-lock* в profiles/ убираются в t.Cleanup.

const rtSaveBinary = "/opt/sbin/mihomo"

const (
	rtSaveOld = "mixed-port: 7891\nmode: rule\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"

	rtSaveGeoip = "mixed-port: 7891\nmode: rule\nproxies: []\nproxy-groups: []\nrules:\n  - GEOIP,CN,DIRECT\n  - MATCH,DIRECT\n"

	rtSaveBadSchema = "mixed-port: 7891\nmode: rule\nproxies: []\nproxy-groups:\n  - name: rt-bad\n    type: rt-no-such-group-type\n    proxies:\n      - DIRECT\nrules:\n  - MATCH,DIRECT\n"

	rtSaveProvider = "mixed-port: 7891\nmode: rule\nproxies: []\nproxy-groups: []\nrule-providers:\n  rt-x:\n    type: file\n    behavior: domain\n    format: text\n    path: ./rule_providers/rt-x.txt\nrules:\n  - RULE-SET,rt-x,DIRECT\n  - MATCH,DIRECT\n"
)

// rtSaveProfilePath — путь файла профиля в корне Mihomo устройства.
func rtSaveProfilePath(name string) string {
	return filepath.Join(rtRtMihomoDir, "profiles", name)
}

// rtSavePrepare создаёт каталог profiles/ при необходимости и регистрирует уборку
// файлов с префиксом (остатки прошлого прогона убираются сразу).
func rtSavePrepare(t *testing.T, prefix string) {
	t.Helper()
	dir := filepath.Join(rtRtMihomoDir, "profiles")
	_, statErr := os.Stat(dir)
	existed := statErr == nil
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("каталог profiles/: %v", err)
	}
	clean := func() {
		files, _ := filepath.Glob(filepath.Join(dir, prefix+"*"))
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		if !existed {
			_ = os.Remove(dir)
		}
	})
}

// rtSavePost отправляет содержимое файла на POST /api/config/save.
func rtSavePost(t *testing.T, p *routertest.Panel, path, content string) int {
	t.Helper()
	status, _, err := p.Request(http.MethodPost, "/api/config/save?path="+url.QueryEscape(path), []byte(content), routertest.AuthFull)
	if err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	return status
}

// rtSaveRequireFile требует, чтобы файл на диске совпал с want побайтно.
func rtSaveRequireFile(t *testing.T, label, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: файл не читается: %v", label, err)
	}
	if !bytes.Equal(got, []byte(want)) {
		t.Errorf("%s: содержимое файла не совпало (на диске %d байт, ожидалось %d)", label, len(got), len(want))
	}
}

// Трасса: профиль с GEOIP-правилом сохраняется (mihomo -t в зеркале корня видит
// геобазы) → ошибка схемы даёт 422 и откат файла → rule-provider type: file без
// файла проходит проверку (ядро провайдеров при -t не грузит). Время ответа на
// GEOIP-правило пишется в журнал: без геобаз проверка недетерминирована (A1).
func TestRouterProfileSaveMirror(t *testing.T) {
	routertest.Current(t)
	if _, err := os.Stat(rtSaveBinary); err != nil {
		t.Skip("Mihomo не установлен на стенде")
	}
	p := routertest.NewPanel(t)
	rtSavePrepare(t, "rt-save")
	path := rtSaveProfilePath("rt-save.yaml")
	if err := os.WriteFile(path, []byte(rtSaveOld), 0o600); err != nil {
		t.Fatalf("исходный файл профиля: %v", err)
	}

	t.Run("geoip", func(t *testing.T) {
		start := time.Now()
		status := rtSavePost(t, p, path, rtSaveGeoip)
		t.Logf("сохранение профиля с GEOIP: %d за %d мс", status, time.Since(start).Milliseconds())
		if status != http.StatusOK {
			t.Fatalf("сохранение профиля с GEOIP: %d, ожидалось 200", status)
		}
		rtSaveRequireFile(t, "после GEOIP", path, rtSaveGeoip)
	})

	t.Run("schema_error_rollback", func(t *testing.T) {
		if status := rtSavePost(t, p, path, rtSaveBadSchema); status != http.StatusUnprocessableEntity {
			t.Fatalf("сохранение с ошибкой схемы: %d, ожидалось 422", status)
		}
		rtSaveRequireFile(t, "после отката", path, rtSaveGeoip)
	})

	t.Run("file_provider_without_file", func(t *testing.T) {
		if status := rtSavePost(t, p, path, rtSaveProvider); status != http.StatusOK {
			t.Fatalf("сохранение с провайдером type: file без файла: %d, ожидалось 200", status)
		}
		rtSaveRequireFile(t, "после провайдера", path, rtSaveProvider)
	})
}
