//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка Preflight Mihomo на настоящей панели устройства (D-24 из 147): профиль
// без external-controller и external-controller-unix даёт предупреждение
// no_external_controller, а не ошибку, поэтому запуск не останавливает окно
// «Ошибка конфигурации». Вердикт — ответ GET /api/config/preflight. Меняется
// только файл, который читает Preflight; ядро не перезапускается. Вид config.yaml
// (ссылка или обычный файл) сохраняется и возвращается в t.Cleanup.

// rtPfCode — код записи Preflight об отсутствии API ядра.
const rtPfCode = "no_external_controller"

// rtPfBody — тело профиля без секций API: только mode и пустые списки.
const rtPfBody = "mode: rule\nproxies: []\nproxy-groups: []\nrules: []\n"

type rtPfIssue struct {
	Code string `json:"code"`
}

type rtPfResult struct {
	Valid    bool        `json:"valid"`
	Errors   []rtPfIssue `json:"errors"`
	Warnings []rtPfIssue `json:"warnings"`
}

func rtPfHas(list []rtPfIssue, code string) bool {
	for _, i := range list {
		if i.Code == code {
			return true
		}
	}
	return false
}

// rtPfPreflight читает ответ Preflight; тело может быть обёрнуто в data.
func rtPfPreflight(t *testing.T, p *routertest.Panel) rtPfResult {
	t.Helper()
	var raw json.RawMessage
	status, err := p.GetJSON("/api/config/preflight?kernel=mihomo", &raw)
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET /api/config/preflight?kernel=mihomo: код %d, ошибка %v", status, err)
	}
	var wrapped struct {
		Data *rtPfResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Data != nil {
		return *wrapped.Data
	}
	var res rtPfResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("ответ Preflight не разобрался: %v", err)
	}
	return res
}

// TestRouterPreflightNoController: профиль Mihomo без external-controller —
// предупреждение no_external_controller, не ошибка.
func TestRouterPreflightNoController(t *testing.T) {
	routertest.Current(t)
	p := routertest.NewPanel(t)

	cfg := filepath.Join(rtRtMihomoDir, "config.yaml")
	link, linkErr := os.Readlink(cfg)
	var data []byte
	mode := os.FileMode(0o600)
	if linkErr != nil {
		var err error
		if data, err = os.ReadFile(cfg); err != nil {
			t.Fatalf("config.yaml Mihomo не читается: %v", err)
		}
		if info, err := os.Stat(cfg); err == nil {
			mode = info.Mode().Perm()
		}
	}
	t.Cleanup(func() {
		tmp := cfg + ".rt-restore"
		_ = os.Remove(tmp)
		var err error
		if linkErr == nil {
			err = os.Symlink(link, tmp)
		} else {
			err = os.WriteFile(tmp, data, mode)
		}
		if err == nil {
			err = os.Rename(tmp, cfg)
		}
		if err != nil {
			t.Errorf("возврат config.yaml: %v", err)
		}
	})

	profile := filepath.Join(t.TempDir(), "rt-preflight-no-api.yaml")
	if err := os.WriteFile(profile, []byte(rtPfBody), 0o600); err != nil {
		t.Fatalf("профиль без API не записывается: %v", err)
	}
	tmp := cfg + ".rt-swap"
	_ = os.Remove(tmp)
	if err := os.Symlink(profile, tmp); err != nil {
		t.Fatalf("ссылка на профиль без API: %v", err)
	}
	if err := os.Rename(tmp, cfg); err != nil {
		t.Fatalf("подмена config.yaml: %v", err)
	}

	res := rtPfPreflight(t, p)
	if rtPfHas(res.Errors, rtPfCode) {
		t.Errorf("%s попал в errors: запуск остановит окно «Ошибка конфигурации»", rtPfCode)
	}
	if !rtPfHas(res.Warnings, rtPfCode) {
		t.Errorf("%s нет в warnings: пользователь не узнает, что API ядра не настроен", rtPfCode)
	}
}
