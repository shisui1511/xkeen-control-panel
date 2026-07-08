package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestConfigSaveRace(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		DataDir:         tmpDir,
		XRayConfigDir:   tmpDir,
		MihomoConfigDir: tmpDir,
		AllowedRoots:    []string{tmpDir},
	}

	subSvc := services.NewSubscriptionService(tmpDir, tmpDir, tmpDir)
	pathVal := utils.NewPathValidator(cfg.AllowedRoots)

	api := &API{
		cfg:             cfg,
		subscriptionSvc: subSvc,
		pathVal:         pathVal,
		configSvc:       services.NewConfigService(tmpDir, cfg.AllowedRoots),
	}

	// Create a dummy config.yaml in the temp directory
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte("external-controller: 0.0.0.0:9090\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write dummy config: %v", err)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Goroutine 1: simulate background subscription refresh locking Mihomo configuration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				subSvc.LockMihomo()
				// Simulate some work
				time.Sleep(1 * time.Millisecond)
				subSvc.UnlockMihomo()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: simulate user saving configuration via API
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				req := httptest.NewRequest(
					http.MethodPost,
					"/api/config/save?path="+url.QueryEscape(configPath),
					strings.NewReader("external-controller: 0.0.0.0:9090\ntproxy-port: 5001\n"),
				)
				rr := httptest.NewRecorder()
				api.ConfigSave(rr, req)
				// We don't verify the result code since the validator binary is not present in tests,
				// we just want to trigger the ConfigSave locking path under -race checker.
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	wg.Wait()
}
