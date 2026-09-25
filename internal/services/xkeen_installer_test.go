package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeInstaller = "#!/bin/sh\n# https://github.com/jameszeroX/XKeen\necho installer \"$1\"\n"

func newTestInstaller(t *testing.T, handler http.HandlerFunc) (*XKeenInstaller, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return &XKeenInstaller{
		URL:    ts.URL + "/install.sh",
		Client: ts.Client(),
		Dir:    t.TempDir(),
	}, ts
}

func TestXKeenInstaller_DownloadFallsBackToMirror(t *testing.T) {
	x, ts := newTestInstaller(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/mirror/") {
			_, _ = w.Write([]byte(fakeInstaller))
			return
		}
		http.Error(w, "blocked", http.StatusForbidden)
	})
	// Зеркало вида https://gh-proxy.com/<полный URL>
	x.Mirrors = []string{"", ts.URL + "/mirror/"}

	path, source, err := x.Download(context.Background())
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !strings.Contains(source, "/mirror/") {
		t.Errorf("source = %q, want mirror", source)
	}
	if got, _ := os.ReadFile(path); string(got) != fakeInstaller {
		t.Errorf("saved script = %q", got)
	}
	if filepath.Dir(path) != x.Dir {
		t.Errorf("script saved outside installer dir: %s", path)
	}
}

// TestXKeenInstaller_RejectsNonInstaller: страница-заглушка зеркала или
// чужой скрипт не запускается как установщик.
func TestXKeenInstaller_RejectsNonInstaller(t *testing.T) {
	for name, body := range map[string]string{
		"html-заглушка": "<html>captcha</html>",
		"чужой скрипт":  "#!/bin/sh\necho hi\n",
	} {
		t.Run(name, func(t *testing.T) {
			x, _ := newTestInstaller(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			})
			x.Mirrors = []string{""}
			if _, _, err := x.Download(context.Background()); err == nil {
				t.Fatal("non-installer response must be rejected")
			}
			if entries, _ := os.ReadDir(x.Dir); len(entries) != 0 {
				t.Errorf("rejected response left files: %v", entries)
			}
		})
	}
}

func TestXKeenInstaller_Command(t *testing.T) {
	x := &XKeenInstaller{Dir: t.TempDir()}
	script := filepath.Join(x.Dir, "xcp-xkeen-install-1.sh")

	if _, err := x.Command(script, "nightly"); err == nil {
		t.Error("unknown channel must be rejected")
	}
	if _, err := x.Command("/etc/evil.sh", "stable"); err == nil {
		t.Error("script outside installer dir must be rejected")
	}
	argv, err := x.Command(script, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if argv[len(argv)-2] != script || argv[len(argv)-1] != "--beta" {
		t.Errorf("path and flag must be positional args, got %q", argv)
	}
}

// TestXKeenInstaller_CommandRunsScriptThenStopsOnFailure: скрипт установщика
// запускается с флагом канала и удаляется; при его ошибке xkeen -i не идёт.
func TestXKeenInstaller_CommandRunsScriptThenStopsOnFailure(t *testing.T) {
	x := &XKeenInstaller{Dir: t.TempDir()}
	script := filepath.Join(x.Dir, "xcp-xkeen-install-1.sh")
	if err := os.WriteFile(script, []byte("echo \"got $1\"; exit 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	argv, err := x.Command(script, "stable")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 3 {
		t.Fatalf("installer exit code must propagate, got %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "got --stable") {
		t.Errorf("installer must get channel flag, output %q", out)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Error("downloaded installer must be removed after run")
	}
}

func TestXKeenInstaller_SingleRun(t *testing.T) {
	x := &XKeenInstaller{}
	release, err := x.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.Acquire(); err != ErrXKeenInstallRunning {
		t.Errorf("second install must be refused, got %v", err)
	}
	release()
	if release2, err := x.Acquire(); err != nil {
		t.Errorf("install must be possible after release: %v", err)
	} else {
		release2()
	}
}
