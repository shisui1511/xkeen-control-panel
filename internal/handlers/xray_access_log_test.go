package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestXrayAccessLogHandlers(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(filepath.Join(dir, "01_log.json"), []byte(`{"log": {"access": "`+logFile+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logFile, []byte("2026/09/24 04:00:01 from 172.16.0.136:1 accepted tcp:youtube.com:443 [tproxy >> proxy]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &API{}
	api.SetXrayAccessLogService(services.NewXrayAccessLogService(dir))

	rec := httptest.NewRecorder()
	api.XrayAccessLog(rec, httptest.NewRequest(http.MethodGet, "/api/xray/access-log?dest=you", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"destination":"youtube.com"`) {
		t.Fatalf("report: %d %s", rec.Code, body)
	}

	rec = httptest.NewRecorder()
	api.XrayAccessLogToggle(rec, httptest.NewRequest(http.MethodPost, "/api/xray/access-log/toggle", strings.NewReader(`{"enabled":false}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body.String())
	}

	empty := &API{}
	empty.SetXrayAccessLogService(services.NewXrayAccessLogService(t.TempDir()))
	rec = httptest.NewRecorder()
	empty.XrayAccessLog(rec, httptest.NewRequest(http.MethodGet, "/api/xray/access-log", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no log config: expected 404, got %d", rec.Code)
	}
}

// Toggle только записывает конфиг: решение о рестарте принимает action=apply,
// поэтому в ответе не должно быть restart_required.
func TestXrayAccessLogToggle_NoRestartRequired(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(filepath.Join(dir, "01_log.json"), []byte(`{"log": {"access": "`+logFile+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &API{}
	api.SetXrayAccessLogService(services.NewXrayAccessLogService(dir))

	// Выключение не трогает системные пути (включение пишет в /opt/var).
	rec := httptest.NewRecorder()
	api.XrayAccessLogToggle(rec, httptest.NewRequest(http.MethodPost, "/api/xray/access-log/toggle", strings.NewReader(`{"enabled":false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status"`) {
		t.Fatalf("в ответе нет status: %s", body)
	}
	if strings.Contains(body, "restart_required") {
		t.Fatalf("ответ содержит restart_required: %s", body)
	}
}
