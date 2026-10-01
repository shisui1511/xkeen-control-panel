package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
)

// stubUpdateSeams подменяет швы performUpdate и возвращает каталог, в котором
// «лежит бинарник панели»: реальный бинарник теста не трогается.
func stubUpdateSeams(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "xcp")
	if err := os.WriteFile(binPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	origFetch, origDownload, origVerify, origPath := releasesFetcher, downloadBinaryFn, verifyChecksumFn, binaryPathFn
	binaryPathFn = func() string { return binPath }
	t.Cleanup(func() {
		releasesFetcher, downloadBinaryFn, verifyChecksumFn, binaryPathFn = origFetch, origDownload, origVerify, origPath
		setUpdateState(UpdateStatus{Status: "idle"})
	})
	setUpdateState(UpdateStatus{Status: "idle"})
	return dir
}

func newUpdateTestAPI(t *testing.T, dataDir string) *API {
	t.Helper()
	srv, err := server.New(&server.Config{Port: 8090}, "v0.29.0", fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &API{srv: srv, cfg: &config.Config{DataDir: dataDir}}
}

func releaseOf(tag string) []githubRelease {
	return []githubRelease{{TagName: tag}}
}

func TestUpdateStatusCodes_Flow(t *testing.T) {
	t.Run("check failed carries the error detail", func(t *testing.T) {
		dir := stubUpdateSeams(t)
		api := newUpdateTestAPI(t, dir)
		releasesFetcher = func() ([]githubRelease, error) { return nil, errors.New("boom") }

		api.performUpdate("stable")

		st := getUpdateState()
		if st.Status != "failed" || st.MessageCode != "check_failed" || st.Params["detail"] != "boom" {
			t.Fatalf("unexpected state: %+v", st)
		}
	})

	t.Run("same version is up to date", func(t *testing.T) {
		dir := stubUpdateSeams(t)
		api := newUpdateTestAPI(t, dir)
		releasesFetcher = func() ([]githubRelease, error) { return releaseOf("v0.29.0"), nil }

		api.performUpdate("stable")

		st := getUpdateState()
		if st.Status != "done" || st.MessageCode != "up_to_date" {
			t.Fatalf("unexpected state: %+v", st)
		}
	})

	t.Run("download failure", func(t *testing.T) {
		dir := stubUpdateSeams(t)
		api := newUpdateTestAPI(t, dir)
		releasesFetcher = func() ([]githubRelease, error) { return releaseOf("v0.30.0"), nil }
		downloadBinaryFn = func(url, dest string, progress progressFunc) error {
			return errors.New("connection reset")
		}

		api.performUpdate("stable")

		st := getUpdateState()
		if st.Status != "failed" || st.MessageCode != "download_failed" || st.Params["detail"] != "connection reset" {
			t.Fatalf("unexpected state: %+v", st)
		}
	})

	t.Run("checksum failure removes the temporary file", func(t *testing.T) {
		dir := stubUpdateSeams(t)
		api := newUpdateTestAPI(t, dir)
		releasesFetcher = func() ([]githubRelease, error) { return releaseOf("v0.30.0"), nil }
		var downloadedTo string
		downloadBinaryFn = func(url, dest string, progress progressFunc) error {
			downloadedTo = dest
			return os.WriteFile(dest, []byte("new"), 0o644)
		}
		verifyChecksumFn = func(filePath, binaryName, checksumURL string) error {
			return errors.New("SHA-256 mismatch")
		}

		api.performUpdate("stable")

		st := getUpdateState()
		if st.Status != "failed" || st.MessageCode != "checksum_failed" || st.Params["detail"] != "SHA-256 mismatch" {
			t.Fatalf("unexpected state: %+v", st)
		}
		if filepath.Dir(downloadedTo) != dir {
			t.Fatalf("temporary file outside the stub dir: %s", downloadedTo)
		}
		if _, err := os.Stat(downloadedTo); !os.IsNotExist(err) {
			t.Fatalf("xcp.new must be removed, stat err = %v", err)
		}
	})
}

func TestSetUpdateStep_ParamsImmutable(t *testing.T) {
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	params := map[string]string{"version": "1"}
	setUpdateStep("downloading", 30, "downloading", params, "msg")
	params["version"] = "2"

	got := getUpdateState()
	if got.Params["version"] != "1" {
		t.Fatalf("params leaked from the caller map: %v", got.Params)
	}
	if got.MessageCode != "downloading" || got.Progress != 30 || got.Timestamp == 0 {
		t.Fatalf("unexpected state: %+v", got)
	}

	setUpdateStep("checking", 10, "checking", nil, "")
	if got := getUpdateState(); got.Params != nil {
		t.Fatalf("params of the previous step must not survive: %v", got.Params)
	}
}

func TestReportDownloadProgress_KeepsCode(t *testing.T) {
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	setUpdateStep("downloading", 30, "downloading", map[string]string{"version": "0.30.0"}, "Downloading update...")
	reportDownloadProgress(50, 100)

	got := getUpdateState()
	if got.MessageCode != "downloading" || got.Params["version"] != "0.30.0" {
		t.Fatalf("code and params must survive progress updates: %+v", got)
	}
	if got.Progress != 42 || got.Downloaded != 50 || got.Total != 100 {
		t.Fatalf("unexpected progress: %+v", got)
	}
}

func TestUpdateEventsSSE_CodeChange(t *testing.T) {
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	base := UpdateStatus{Status: "installing", Message: "same", Progress: 60, MessageCode: "a"}
	setUpdateState(base)

	api := &API{}
	ts := httptest.NewServer(http.HandlerFunc(api.UpdateEventsSSE))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	events := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				events <- line
			}
		}
		close(events)
	}()

	next := func() string {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before the event")
			}
			return ev
		case <-time.After(2 * time.Second):
			t.Fatal("no SSE event within 2s")
			return ""
		}
	}

	if first := next(); !strings.Contains(first, `"message_code":"a"`) {
		t.Fatalf("first event: %s", first)
	}

	// Тот же status/progress/message, меняется только код
	base.MessageCode = "b"
	setUpdateState(base)
	if second := next(); !strings.Contains(second, `"message_code":"b"`) {
		t.Fatalf("second event: %s", second)
	}

	// Только параметры
	base.Params = map[string]string{"version": "1"}
	setUpdateState(base)
	if third := next(); !strings.Contains(third, `"version":"1"`) {
		t.Fatalf("third event: %s", third)
	}
}

// errorEnvelope — разбор конверта ошибки APIResponse в тестах.
type errorEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code"`
	Detail  string `json:"detail"`
}

// callUpdateHandler вызывает обработчик за i18n.Middleware с языком запроса
// из Accept-Language и разбирает конверт ответа.
func callUpdateHandler(t *testing.T, h http.HandlerFunc, method, target, body, lang string) (int, errorEnvelope) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Header.Set("Accept-Language", lang)
	rec := httptest.NewRecorder()
	i18n.Middleware(h).ServeHTTP(rec, req)
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not a JSON envelope: %v (%q)", err, rec.Body.String())
	}
	return rec.Code, env
}

func TestUpdateErrors_InProgress(t *testing.T) {
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })
	setUpdateState(UpdateStatus{Status: "downloading"})

	api := newUpdateTestAPI(t, t.TempDir())
	handlers := map[string]http.HandlerFunc{
		"install":  api.UpdateInstall,
		"rollback": api.UpdateRollback,
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			texts := map[string]string{}
			for _, lang := range []string{"ru", "en"} {
				status, env := callUpdateHandler(t, h, http.MethodPost, "/api/update/"+name, "", lang)
				if status != http.StatusConflict {
					t.Fatalf("%s: status = %d, want 409", lang, status)
				}
				if env.Success || env.Code != "update_in_progress" {
					t.Fatalf("%s: unexpected envelope %+v", lang, env)
				}
				if want := i18n.T(lang, "update.in_progress"); env.Error != want {
					t.Fatalf("%s: error = %q, want %q", lang, env.Error, want)
				}
				texts[lang] = env.Error
			}
			if texts["ru"] == texts["en"] {
				t.Fatalf("ru and en texts must differ: %q", texts["ru"])
			}
		})
	}
}

// unwritableConfigPath — путь, родитель которого обычный файл: config.Save
// на нём гарантированно падает.
func unwritableConfigPath(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(file, "config.json")
}

// windowErrorText — текст ошибки parseInstallWindow, который уходит в detail.
func windowErrorText(window string) string {
	_, _, err := parseInstallWindow(window)
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestUpdateErrors(t *testing.T) {
	type tc struct {
		name       string
		call       func(api *API) http.HandlerFunc
		method     string
		target     string
		body       string
		prepare    func(t *testing.T)
		cfgPath    func(t *testing.T) string
		status     int
		code       string
		key        string
		detail     string
		wantDetail bool
	}
	cases := []tc{
		{
			name: "rollback: bad body", method: http.MethodPost, target: "/api/update/rollback", body: "{bad",
			call:   func(a *API) http.HandlerFunc { return a.UpdateRollback },
			status: http.StatusBadRequest, code: "invalid_request_body", key: "error.invalid_request",
		},
		{
			name: "rollback: no backup", method: http.MethodPost, target: "/api/update/rollback",
			call:   func(a *API) http.HandlerFunc { return a.UpdateRollback },
			status: http.StatusNotFound, code: "update_no_backup", key: "update.no_backup",
		},
		{
			name: "changelog: version required", method: http.MethodGet, target: "/api/update/changelog",
			call:   func(a *API) http.HandlerFunc { return a.UpdateChangelog },
			status: http.StatusBadRequest, code: "update_version_required", key: "update.version_required",
		},
		{
			name: "check: fetch failed carries detail", method: http.MethodGet, target: "/api/update/check",
			call: func(a *API) http.HandlerFunc { return a.UpdateCheck },
			prepare: func(t *testing.T) {
				releasesFetcher = func() ([]githubRelease, error) { return nil, errors.New("GitHub API: 403 rate limit") }
			},
			status: http.StatusInternalServerError, code: "update_check_failed", key: "update.check_failed",
			detail: "GitHub API: 403 rate limit", wantDetail: true,
		},
		{
			name: "channel: bad body", method: http.MethodPost, target: "/api/update/channel", body: "{invalid",
			call:   func(a *API) http.HandlerFunc { return a.UpdateChannelSet },
			status: http.StatusBadRequest, code: "invalid_request_body", key: "error.invalid_request",
		},
		{
			name: "channel: not in the whitelist", method: http.MethodPost, target: "/api/update/channel", body: `{"channel":"alpha"}`,
			call:   func(a *API) http.HandlerFunc { return a.UpdateChannelSet },
			status: http.StatusBadRequest, code: "update_channel_invalid", key: "update.channel_invalid",
		},
		{
			name: "channel: save failed carries detail", method: http.MethodPost, target: "/api/update/channel", body: `{"channel":"beta"}`,
			call:    func(a *API) http.HandlerFunc { return a.UpdateChannelSet },
			cfgPath: unwritableConfigPath,
			status:  http.StatusInternalServerError, code: "update_save_failed", key: "update.save_failed",
			wantDetail: true,
		},
		{
			name: "settings: bad body", method: http.MethodPost, target: "/api/update/settings", body: "{x",
			call:   func(a *API) http.HandlerFunc { return a.UpdateSettingsHandler },
			status: http.StatusBadRequest, code: "invalid_request_body", key: "error.invalid_request",
		},
		{
			name: "settings: invalid window carries detail", method: http.MethodPost, target: "/api/update/settings", body: `{"install_window":"25:00-01:00"}`,
			call:   func(a *API) http.HandlerFunc { return a.UpdateSettingsHandler },
			status: http.StatusBadRequest, code: "update_window_invalid", key: "update.window_invalid",
			detail: windowErrorText("25:00-01:00"), wantDetail: true,
		},
		{
			name: "settings: save failed carries detail", method: http.MethodPost, target: "/api/update/settings", body: `{"auto_check":true}`,
			call:    func(a *API) http.HandlerFunc { return a.UpdateSettingsHandler },
			cfgPath: unwritableConfigPath,
			status:  http.StatusInternalServerError, code: "update_save_failed", key: "update.save_failed",
			wantDetail: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := stubUpdateSeams(t)
			api := newUpdateTestAPI(t, dir)
			if c.prepare != nil {
				c.prepare(t)
			}
			if c.cfgPath != nil {
				api.cfg.ConfigPath = c.cfgPath(t)
			}
			for _, lang := range []string{"ru", "en"} {
				status, env := callUpdateHandler(t, c.call(api), c.method, c.target, c.body, lang)
				if status != c.status {
					t.Fatalf("%s: status = %d, want %d", lang, status, c.status)
				}
				if env.Success || env.Code != c.code {
					t.Fatalf("%s: unexpected envelope %+v", lang, env)
				}
				if want := i18n.T(lang, c.key); env.Error != want {
					t.Fatalf("%s: error = %q, want %q", lang, env.Error, want)
				}
				if c.wantDetail {
					if c.detail != "" && env.Detail != c.detail {
						t.Fatalf("%s: detail = %q, want %q", lang, env.Detail, c.detail)
					}
					if env.Detail == "" {
						t.Fatalf("%s: detail must not be empty", lang)
					}
					if strings.Contains(env.Error, env.Detail) {
						t.Fatalf("%s: error must not contain the technical detail: %q", lang, env.Error)
					}
				} else if env.Detail != "" {
					t.Fatalf("%s: unexpected detail %q", lang, env.Detail)
				}
			}
			if i18n.T("ru", c.key) == i18n.T("en", c.key) {
				t.Fatalf("ru and en texts of %s must differ", c.key)
			}
		})
	}
}
