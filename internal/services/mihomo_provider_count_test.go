package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newProviderCountService поднимает httptest-сервер как external-controller в
// config.yaml временного каталога.
func newProviderCountService(t *testing.T, secret string, h http.HandlerFunc) *MihomoService {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfg := fmt.Sprintf("external-controller: %s\n", strings.TrimPrefix(srv.URL, "http://"))
	if secret != "" {
		cfg += fmt.Sprintf("secret: %q\n", secret)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewMihomoService("", "", dir)
}

func TestMihomoService_ProviderCount(t *testing.T) {
	var gotAuth, gotMethod string
	var gotPaths []string
	svc := newProviderCountService(t, "s3", func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMethod = r.Header.Get("Authorization"), r.Method
		gotPaths = append(gotPaths, r.URL.EscapedPath())
		switch r.URL.EscapedPath() {
		case "/providers/proxies/xcp-a":
			_, _ = w.Write([]byte(`{"name":"xcp-a","proxies":[{},{}]}`))
		case "/providers/rules/xcp-r":
			_, _ = w.Write([]byte(`{"ruleCount":5}`))
		case "/providers/proxies/xcp-empty":
			w.WriteHeader(http.StatusNoContent)
		case "/providers/proxies/xcp-bad":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
		case "/providers/proxies/a%20b%2Fc":
			_, _ = w.Write([]byte(`{"proxies":[{}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	if n, err := svc.ProviderCount(context.Background(), "proxies", "xcp-a"); err != nil || n != 2 {
		t.Fatalf("proxies = %d, %v; want 2, nil", n, err)
	}
	if gotAuth != "Bearer s3" || gotMethod != http.MethodGet {
		t.Errorf("метод %q, заголовок %q; want GET и Bearer s3", gotMethod, gotAuth)
	}
	if n, err := svc.ProviderCount(context.Background(), "rules", "xcp-r"); err != nil || n != 5 {
		t.Fatalf("rules = %d, %v; want 5, nil", n, err)
	}
	if _, err := svc.ProviderCount(context.Background(), "proxies", "xcp-empty"); !errors.Is(err, ErrProviderEmpty) {
		t.Errorf("204: err = %v, want ErrProviderEmpty", err)
	}
	_, err := svc.ProviderCount(context.Background(), "proxies", "xcp-bad")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("503: err = %v, want ошибка с кодом 503", err)
	}
	if len(err.Error()) > 450 {
		t.Errorf("тело ошибки не ограничено: %d байт", len(err.Error()))
	}
	if n, err := svc.ProviderCount(context.Background(), "proxies", "a b/c"); err != nil || n != 1 {
		t.Errorf("экранирование имени: %d, %v; want 1, nil (пути: %v)", n, err, gotPaths)
	}
	if _, err := svc.ProviderCount(context.Background(), "weird", "x"); err == nil {
		t.Error("неизвестный тип провайдера принят")
	}
}

// Точечного GET у rule-провайдера может не быть: тогда число правил берётся из
// общего списка.
func TestMihomoService_ProviderCountRulesListFallback(t *testing.T) {
	svc := newProviderCountService(t, "", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/providers/rules":
			_, _ = w.Write([]byte(`{"providers":{"xcp-r":{"ruleCount":7},"other":{"ruleCount":1}}}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	if n, err := svc.ProviderCount(context.Background(), "rules", "xcp-r"); err != nil || n != 7 {
		t.Fatalf("rules через список = %d, %v; want 7, nil", n, err)
	}
	if _, err := svc.ProviderCount(context.Background(), "rules", "missing"); err == nil {
		t.Error("провайдер, которого нет в списке, не дал ошибку")
	}
}
