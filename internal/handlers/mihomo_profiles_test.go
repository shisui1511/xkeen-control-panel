package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestMihomoProfileHandlers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mihomo")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	svc := services.NewMihomoProfileService(dir, filepath.Join(root, "xcp"))
	api.SetMihomoProfileService(svc)
	// No core in tests: skip validation and restarts.
	svc.Validate = func(string) error { return nil }
	svc.CoreActive = func() bool { return false }

	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if path == "/api/mihomo/profiles" {
			api.MihomoProfiles(rec, req)
		} else {
			api.MihomoProfileAction(rec, req)
		}
		return rec
	}

	if rec := call(http.MethodGet, "/api/mihomo/profiles", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"default"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"work"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"../etc"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad name: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/delete", `{"name":"default"}`); rec.Code != http.StatusConflict {
		t.Fatalf("delete active: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"work"`) {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("activate missing: %d", rec.Code)
	}
	if rec := call(http.MethodGet, "/api/mihomo/profiles/create", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET action: %d", rec.Code)
	}
}

// TestMihomoProfileActivate_CoreActiveByApplier: активация профиля перезапускает
// ядро, только если Mihomo — активное ядро и его процесс может работать (unknown
// считается запущенным). Иначе ответ несёт outcome, а откат при нездоровом ядре
// работает как раньше.
func TestMihomoProfileActivate_CoreActiveByApplier(t *testing.T) {
	cases := []struct {
		name         string
		configured   string
		xrayStatus   string
		mihomoStatus string
		healthy      bool
		wantRestarts int32
		wantOutcome  string
		wantRestart  bool
		wantRollback bool
		wantActive   string
	}{
		{name: "mihomo running", configured: "mihomo", xrayStatus: "stopped", mihomoStatus: "running", healthy: true, wantRestarts: 1, wantRestart: true, wantActive: "work"},
		{name: "mihomo unknown counts as running", configured: "mihomo", xrayStatus: "stopped", mihomoStatus: "unknown", healthy: true, wantRestarts: 1, wantRestart: true, wantActive: "work"},
		{name: "mihomo stopped", configured: "mihomo", xrayStatus: "stopped", mihomoStatus: "stopped", healthy: true, wantOutcome: "saved_kernel_stopped", wantActive: "work"},
		{name: "xray active", configured: "xray", xrayStatus: "running", mihomoStatus: "stopped", healthy: true, wantOutcome: "saved_kernel_inactive", wantActive: "work"},
		{name: "unhealthy core rolls back", configured: "mihomo", xrayStatus: "stopped", mihomoStatus: "running", healthy: false, wantRestarts: 2, wantRestart: true, wantRollback: true, wantActive: "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "mihomo")
			if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"default", "work"} {
				if err := os.WriteFile(filepath.Join(dir, "profiles", n+".yaml"), []byte("mode: rule\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
				t.Fatal(err)
			}

			api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
			api.kernelApplier = services.NewKernelApplierFunc(
				func(name string) string {
					switch name {
					case "mihomo":
						return tc.mihomoStatus
					case "xray":
						return tc.xrayStatus
					}
					return "not_installed"
				},
				func() string { return tc.configured },
				func() (string, error) { return "", nil },
			)
			svc := services.NewMihomoProfileService(dir, filepath.Join(root, "xcp"))
			api.SetMihomoProfileService(svc)
			svc.Validate = func(string) error { return nil }
			var restarts int32
			svc.Restart = func() error {
				atomic.AddInt32(&restarts, 1)
				return nil
			}
			svc.Healthy = func() bool { return tc.healthy }

			rec := httptest.NewRecorder()
			api.MihomoProfileAction(rec, httptest.NewRequest(http.MethodPost, "/api/mihomo/profiles/activate", strings.NewReader(`{"name":"work"}`)))
			if rec.Code != http.StatusOK {
				t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
			}
			var env struct {
				Data struct {
					Active     string `json:"active"`
					Restarted  bool   `json:"restarted"`
					RolledBack bool   `json:"rolled_back"`
					Outcome    string `json:"outcome"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v: %s", err, rec.Body.String())
			}
			if got := atomic.LoadInt32(&restarts); got != tc.wantRestarts {
				t.Errorf("restarts = %d, want %d", got, tc.wantRestarts)
			}
			if env.Data.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", env.Data.Outcome, tc.wantOutcome)
			}
			if env.Data.Restarted != tc.wantRestart {
				t.Errorf("restarted = %t, want %t", env.Data.Restarted, tc.wantRestart)
			}
			if env.Data.RolledBack != tc.wantRollback {
				t.Errorf("rolled_back = %t, want %t", env.Data.RolledBack, tc.wantRollback)
			}
			if env.Data.Active != tc.wantActive {
				t.Errorf("active = %q, want %q", env.Data.Active, tc.wantActive)
			}
		})
	}
}

// newActivationFixture — каталог Mihomo с профилями default (активный) и work.
func newActivationFixture(t *testing.T) (dir, dataDir string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "mihomo")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"default", "work"} {
		if err := os.WriteFile(filepath.Join(dir, "profiles", n+".yaml"), []byte("mode: rule\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(root, "xcp")
}

// TestMihomoProfileActivate_RestartSerialisedByApplier (WR-02): рестарт при
// активации профиля идёт через KernelApplier, то есть под его мьютексом и с
// решением по свежему статусу. Пока другое применение держит рестарт,
// активация не запускает второй xkeen -restart, а после освобождения видит
// остановленное ядро и не перезапускает его (профиль остаётся активным).
func TestMihomoProfileActivate_RestartSerialisedByApplier(t *testing.T) {
	dir, dataDir := newActivationFixture(t)

	var (
		statusMu     sync.Mutex
		mihomoStatus = "running"
		running      int32
		maxRunning   int32
		restarts     int32
	)
	firstEntered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	api.kernelApplier = services.NewKernelApplierFunc(
		func(name string) string {
			statusMu.Lock()
			defer statusMu.Unlock()
			if name == "mihomo" {
				return mihomoStatus
			}
			return "not_installed"
		},
		func() string { return "mihomo" },
		func() (string, error) {
			cur := atomic.AddInt32(&running, 1)
			for {
				m := atomic.LoadInt32(&maxRunning)
				if cur <= m || atomic.CompareAndSwapInt32(&maxRunning, m, cur) {
					break
				}
			}
			atomic.AddInt32(&restarts, 1)
			once.Do(func() {
				close(firstEntered)
				<-release
			})
			atomic.AddInt32(&running, -1)
			return "", nil
		},
	)
	svc := services.NewMihomoProfileService(dir, dataDir)
	api.SetMihomoProfileService(svc)
	svc.Validate = func(string) error { return nil }

	// Лишь после предварительной проверки CoreActive активация упирается в
	// мьютекс применения.
	coreActive := svc.CoreActive
	decided := make(chan struct{})
	svc.CoreActive = func() bool {
		ok := coreActive()
		close(decided)
		return ok
	}

	// Чужое применение (например, обновление подписки) уже держит рестарт.
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		api.kernelApplier.Apply("mihomo")
	}()
	<-firstEntered

	var rec *httptest.ResponseRecorder
	activated := make(chan struct{})
	go func() {
		defer close(activated)
		rec = httptest.NewRecorder()
		api.MihomoProfileAction(rec, httptest.NewRequest(http.MethodPost, "/api/mihomo/profiles/activate", strings.NewReader(`{"name":"work"}`)))
	}()
	<-decided
	time.Sleep(50 * time.Millisecond)

	// Ядро остановили, пока шёл чужой рестарт.
	statusMu.Lock()
	mihomoStatus = "stopped"
	statusMu.Unlock()
	close(release)
	<-otherDone
	<-activated

	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Active     string `json:"active"`
			Restarted  bool   `json:"restarted"`
			RolledBack bool   `json:"rolled_back"`
			Outcome    string `json:"outcome"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if got := atomic.LoadInt32(&maxRunning); got != 1 {
		t.Errorf("одновременных рестартов = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&restarts); got != 1 {
		t.Errorf("рестартов = %d, want 1 (только чужой: активация видит остановленное ядро)", got)
	}
	if env.Data.Restarted || env.Data.RolledBack {
		t.Errorf("restarted=%t rolled_back=%t, want false/false", env.Data.Restarted, env.Data.RolledBack)
	}
	if env.Data.Active != "work" {
		t.Errorf("active = %q, want work", env.Data.Active)
	}
	if env.Data.Outcome != "saved_kernel_stopped" {
		t.Errorf("outcome = %q, want saved_kernel_stopped", env.Data.Outcome)
	}
}

// TestMihomoProfileActivate_RestartFailureRollsBack: реальный хук Restart
// переводит restart_failed приложения в ошибку, и Activate откатывает профиль.
func TestMihomoProfileActivate_RestartFailureRollsBack(t *testing.T) {
	dir, dataDir := newActivationFixture(t)
	var restarts int32
	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	api.kernelApplier = services.NewKernelApplierFunc(
		func(name string) string {
			if name == "mihomo" {
				return "running"
			}
			return "not_installed"
		},
		func() string { return "mihomo" },
		func() (string, error) {
			// Первый рестарт (новый профиль) падает, второй (откат) проходит.
			if atomic.AddInt32(&restarts, 1) == 1 {
				return "boom", errors.New("exit status 1")
			}
			return "", nil
		},
	)
	svc := services.NewMihomoProfileService(dir, dataDir)
	api.SetMihomoProfileService(svc)
	svc.Validate = func(string) error { return nil }

	res, err := svc.Activate("work")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restarted || !res.RolledBack || res.Active != "default" || res.Error != "boom" {
		t.Errorf("got %+v, want restarted+rolled_back to default with error boom", res)
	}
	if got := atomic.LoadInt32(&restarts); got != 2 {
		t.Errorf("restarts = %d, want 2 (новый профиль и откат)", got)
	}
}
