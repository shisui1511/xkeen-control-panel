package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// Method and availability guards of the profile, rule-provider and Xray
// access-log handlers.
func TestNewHandlersGuards(t *testing.T) {
	api := &API{}
	cases := []struct {
		name   string
		h      http.HandlerFunc
		method string
		want   int
	}{
		{"profiles method", api.MihomoProfiles, http.MethodPost, http.StatusMethodNotAllowed},
		{"profiles unavailable", api.MihomoProfiles, http.MethodGet, http.StatusServiceUnavailable},
		{"profile action method", api.MihomoProfileAction, http.MethodGet, http.StatusMethodNotAllowed},
		{"profile action unavailable", api.MihomoProfileAction, http.MethodPost, http.StatusServiceUnavailable},
		{"providers info method", api.RuleProvidersInfo, http.MethodPost, http.StatusMethodNotAllowed},
		{"providers info unavailable", api.RuleProvidersInfo, http.MethodGet, http.StatusServiceUnavailable},
		{"provider content method", api.RuleProviderContent, http.MethodPost, http.StatusMethodNotAllowed},
		{"provider content unavailable", api.RuleProviderContent, http.MethodGet, http.StatusServiceUnavailable},
		{"check url method", api.RuleProviderCheckURL, http.MethodGet, http.StatusMethodNotAllowed},
		{"check url unavailable", api.RuleProviderCheckURL, http.MethodPost, http.StatusServiceUnavailable},
		{"access log method", api.XrayAccessLog, http.MethodPost, http.StatusMethodNotAllowed},
		{"access log unavailable", api.XrayAccessLog, http.MethodGet, http.StatusServiceUnavailable},
		{"access toggle method", api.XrayAccessLogToggle, http.MethodGet, http.StatusMethodNotAllowed},
		{"access toggle unavailable", api.XrayAccessLogToggle, http.MethodPost, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		c.h(rec, httptest.NewRequest(c.method, "/api/x", strings.NewReader("{}")))
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestValidateMihomoProfile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	if err := api.validateMihomoProfile("x.yaml"); err != nil {
		t.Fatalf("no binary configured: %v", err)
	}

	api.cfg.MihomoBinary = filepath.Join(dir, "missing")
	if err := api.validateMihomoProfile("x.yaml"); err != nil {
		t.Fatalf("binary not installed: %v", err)
	}

	api.cfg.MihomoBinary = write("ok", "exit 0")
	if err := api.validateMihomoProfile("x.yaml"); err != nil {
		t.Fatalf("valid profile: %v", err)
	}

	api.cfg.MihomoBinary = write("bad", "echo 'parse config error'; exit 1")
	err := api.validateMihomoProfile("x.yaml")
	if err == nil || !strings.Contains(err.Error(), "parse config error") {
		t.Fatalf("invalid profile: %v", err)
	}

	if !(&API{}).waitMihomoRunning() {
		t.Fatal("without kernel service the core is assumed running")
	}
}
