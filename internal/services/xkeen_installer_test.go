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
	if argv[len(argv)-3] != script || argv[len(argv)-2] != "--beta" {
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

// TestXKeenInstaller_CommandSetupOnce: install.sh сам запускает `xkeen -i`;
// панель повторяет настройку, только если init-скрипт XKeen не появился.
func TestXKeenInstaller_CommandSetupOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		initExist bool
		wantSetup bool
	}{
		{"setup done by install.sh", true, false},
		{"setup interrupted", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initDir := filepath.Join(dir, "init.d")
			binDir := filepath.Join(dir, "bin")
			for _, d := range []string{initDir, binDir} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.initExist {
				if err := os.WriteFile(filepath.Join(initDir, xkeenInitScript), nil, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(binDir, "xkeen"), []byte("#!/bin/sh\necho \"setup $1\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			x := &XKeenInstaller{Dir: dir, InitDir: initDir}
			script := filepath.Join(dir, "xcp-xkeen-install-1.sh")
			if err := os.WriteFile(script, []byte("echo installed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			argv, err := x.Command(script, "stable")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run: %v (%s)", err, out)
			}
			if got := strings.Contains(string(out), "setup -i"); got != tc.wantSetup {
				t.Errorf("xkeen -i ran=%v, want %v; output %q", got, tc.wantSetup, out)
			}
			if x.SetupComplete() != tc.initExist {
				t.Errorf("SetupComplete()=%v, want %v", x.SetupComplete(), tc.initExist)
			}
		})
	}
}

// TestXKeenInstaller_CommandShell: оболочка Entware важнее /bin/sh (на
// Keenetic это NDM Shell Wrapper, теряющий аргументы `sh -c`), явная — важнее всех.
func TestXKeenInstaller_CommandShell(t *testing.T) {
	dir := t.TempDir()
	entware := filepath.Join(dir, "opt-sh")
	if err := os.WriteFile(entware, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	saved := xkeenShellCandidates
	t.Cleanup(func() { xkeenShellCandidates = saved })

	x := &XKeenInstaller{Dir: dir}
	script := filepath.Join(dir, "xcp-xkeen-install-1.sh")

	xkeenShellCandidates = []string{filepath.Join(dir, "missing"), entware, "/bin/sh"}
	if argv, _ := x.Command(script, "stable"); argv[0] != entware {
		t.Errorf("first existing candidate must be used, got %q", argv[0])
	}
	xkeenShellCandidates = []string{filepath.Join(dir, "missing")}
	if argv, _ := x.Command(script, "stable"); argv[0] != "/bin/sh" {
		t.Errorf("fallback must be /bin/sh, got %q", argv[0])
	}
	x.Shell = "/custom/sh"
	if argv, _ := x.Command(script, "stable"); argv[0] != "/custom/sh" {
		t.Errorf("explicit Shell must win, got %q", argv[0])
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
