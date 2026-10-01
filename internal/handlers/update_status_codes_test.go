package handlers

import (
	"bufio"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
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
