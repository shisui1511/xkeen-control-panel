//go:build router

package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Чтение access-лога Xray идёт только по пути внутри /opt/var/log. Вердикт —
// статус и отчёт сервиса на настоящем файле устройства, эталонных данных нет.

const rtGuardLine = "2026/09/24 03:59:04 from 192.0.2.10:52344 accepted tcp:example.org:443 [tproxy-in >> proxy]\n"

func rtGuardWriteConfig(t *testing.T, dir, access string) {
	t.Helper()
	body := `{"log": {"access": "` + access + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "01_log.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("запись конфига: %v", err)
	}
}

func TestRouterAccessLogPathGuard(t *testing.T) {
	tg := routertest.Current(t)

	confDir, err := os.MkdirTemp(tg.WorkDir, "guard-conf-")
	if err != nil {
		t.Fatalf("каталог конфигов: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(confDir) })

	logDir := filepath.Join(utils.LogRoot, "xcp-rt-guard")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("каталог лога: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(logDir) })
	insidePath := filepath.Join(logDir, "a.log")
	if err := os.WriteFile(insidePath, []byte(rtGuardLine), 0o644); err != nil {
		t.Fatalf("запись лога: %v", err)
	}
	// Файл вне /opt/var/log, но внутри допустимого корня валидатора.
	outsidePath := filepath.Join(confDir, "outside.log")
	if err := os.WriteFile(outsidePath, []byte(rtGuardLine), 0o644); err != nil {
		t.Fatalf("запись внешнего лога: %v", err)
	}

	newSvc := func(withValidator bool) *XrayAccessLogService {
		svc := NewXrayAccessLogService(confDir)
		if withValidator {
			svc.PathVal = utils.NewPathValidator([]string{utils.LogRoot, tg.WorkDir})
		}
		return svc
	}

	t.Run("inside", func(t *testing.T) {
		rtGuardWriteConfig(t, confDir, insidePath)
		svc := newSvc(true)
		st, err := svc.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !st.Enabled || st.PathDenied || st.FileSize <= 0 {
			t.Fatalf("путь внутри /opt/var/log: enabled=%v denied=%v size=%d", st.Enabled, st.PathDenied, st.FileSize)
		}
		rep, err := svc.ReportOf(st, XrayAccessFilter{})
		if err != nil {
			t.Fatalf("ReportOf: %v", err)
		}
		if len(rep.Entries) != 1 {
			t.Fatalf("записей %d, ждали 1", len(rep.Entries))
		}
	})

	t.Run("outside_root", func(t *testing.T) {
		rtGuardWriteConfig(t, confDir, outsidePath)
		svc := newSvc(true)
		st, err := svc.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !st.Enabled || !st.PathDenied || st.FileSize != 0 {
			t.Fatalf("путь вне /opt/var/log: enabled=%v denied=%v size=%d", st.Enabled, st.PathDenied, st.FileSize)
		}
		if st.AccessPath != outsidePath {
			t.Fatalf("путь в статусе искажён: %q", st.AccessPath)
		}
		rep, err := svc.ReportOf(st, XrayAccessFilter{})
		if err != nil {
			t.Fatalf("ReportOf: %v", err)
		}
		if len(rep.Entries) != 0 {
			t.Fatalf("записей %d при отказе, ждали 0", len(rep.Entries))
		}
	})

	t.Run("traversal", func(t *testing.T) {
		rtGuardWriteConfig(t, confDir, logDir+"/../../../etc/passwd")
		st, err := newSvc(true).Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !st.PathDenied || st.FileSize != 0 {
			t.Fatalf("переход вверх: denied=%v size=%d", st.PathDenied, st.FileSize)
		}
	})

	t.Run("none", func(t *testing.T) {
		rtGuardWriteConfig(t, confDir, "none")
		st, err := newSvc(true).Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.Enabled || st.PathDenied {
			t.Fatalf("access none: enabled=%v denied=%v", st.Enabled, st.PathDenied)
		}
		if st.WillWrite != DefaultXrayAccessLog {
			t.Fatalf("will_write при выключенном флаге: %q", st.WillWrite)
		}
	})

	t.Run("nil_validator", func(t *testing.T) {
		rtGuardWriteConfig(t, confDir, insidePath)
		st, err := newSvc(false).Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !st.PathDenied || st.FileSize != 0 {
			t.Fatalf("без валидатора: denied=%v size=%d", st.PathDenied, st.FileSize)
		}
	})
}
