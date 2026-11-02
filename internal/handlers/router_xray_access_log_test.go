//go:build router

package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Включение access-лога на секции "log" без ключа access: правка конфига Xray
// должна оставаться корректным JSON. Вердикт — json.Valid результата и значения,
// которые читает сам парсер, эталонных файлов нет.

func TestRouterAccessLogLegacyEmptySection(t *testing.T) {
	tg := routertest.Current(t)

	cases := []struct {
		name string
		body string
		keep map[string]string
	}{
		{"empty_section", `{"log": {}}`, nil},
		{"empty_with_space", "{\"log\": {\n  }\n}", nil},
		{"loglevel_kept", `{"log": {"loglevel":"warning"}}`, map[string]string{"loglevel": "warning"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(tg.WorkDir, "accesslog-")
			if err != nil {
				t.Fatalf("каталог конфигов: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			file := filepath.Join(dir, "01_log.json")
			if err := os.WriteFile(file, []byte(c.body), 0o644); err != nil {
				t.Fatalf("запись конфига: %v", err)
			}

			svc := services.NewXrayAccessLogService(dir)
			if _, err := svc.SetEnabled(true); err != nil {
				t.Fatalf("SetEnabled(true): %v", err)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("чтение конфига: %v", err)
			}
			if !json.Valid(data) {
				t.Fatalf("файл после включения не JSON: %s", data)
			}
			var root struct {
				Log map[string]string `json:"log"`
			}
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatalf("разбор файла: %v", err)
			}
			if got := root.Log["access"]; got != services.DefaultXrayAccessLog {
				t.Fatalf("access = %q, ждали %q", got, services.DefaultXrayAccessLog)
			}
			for k, v := range c.keep {
				if root.Log[k] != v {
					t.Fatalf("ключ %s = %q, ждали %q", k, root.Log[k], v)
				}
			}
		})
	}
}
