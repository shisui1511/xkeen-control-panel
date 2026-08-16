package services

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPTYService_DetectShell(t *testing.T) {
	svc := NewPTYService()
	shell := svc.detectShell()
	if shell == "" {
		t.Fatal("expected non-empty shell path")
	}
}

func TestPTYService_StartSession(t *testing.T) {
	svc := NewPTYService()
	sess, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start PTY session: %v", err)
	}
	defer sess.Close()

	if sess.ID == "" {
		t.Error("expected non-empty session ID")
	}

	if count := svc.ActiveSessionsCount(); count != 1 {
		t.Errorf("expected 1 active session, got %d", count)
	}

	// Write command and read output
	_, err = sess.Write([]byte("echo PTY_HELLO_WORLD\n"))
	if err != nil {
		t.Fatalf("failed to write to PTY: %v", err)
	}

	buf := make([]byte, 1024)
	outputReceived := false
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		n, err := sess.Read(buf)
		if err != nil {
			break
		}
		if bytes.Contains(buf[:n], []byte("PTY_HELLO_WORLD")) {
			outputReceived = true
			break
		}
	}

	if !outputReceived {
		t.Error("expected output to contain PTY_HELLO_WORLD")
	}
}

func TestPTYService_Resize(t *testing.T) {
	svc := NewPTYService()
	sess, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start PTY session: %v", err)
	}
	defer sess.Close()

	if err := sess.Resize(120, 40); err != nil {
		t.Fatalf("failed to resize PTY session: %v", err)
	}

	if err := sess.Resize(0, 0); err == nil {
		t.Error("expected error when resizing to invalid dimensions")
	}

	// Large dimensions should be clamped without error
	if err := sess.Resize(70000, 1000); err != nil {
		t.Fatalf("expected oversized resize to be clamped, got error: %v", err)
	}
}

func TestPTYService_StartSession_OversizedDimensions(t *testing.T) {
	svc := NewPTYService()
	sess, err := svc.StartSession(70000, 1000)
	if err != nil {
		t.Fatalf("failed to start session with oversized dimensions: %v", err)
	}
	defer sess.Close()
}

func TestPTYService_MaxSessionsLimit(t *testing.T) {
	svc := NewPTYService()

	sess1, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start session 1: %v", err)
	}
	defer sess1.Close()

	sess2, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start session 2: %v", err)
	}
	defer sess2.Close()

	if count := svc.ActiveSessionsCount(); count != 2 {
		t.Errorf("expected 2 active sessions, got %d", count)
	}

	// Try 3rd session
	sess3, err := svc.StartSession(80, 24)
	if err != ErrMaxSessionsReached {
		if sess3 != nil {
			sess3.Close()
		}
		t.Fatalf("expected ErrMaxSessionsReached, got: %v", err)
	}

	// Close sess1
	_ = sess1.Close()
	if count := svc.ActiveSessionsCount(); count != 1 {
		t.Errorf("expected 1 active session after closing one, got %d", count)
	}

	// Now 3rd session should succeed
	sess3, err = svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start session after releasing slot: %v", err)
	}
	defer sess3.Close()
}

func TestPTYService_CloseAndCleanup(t *testing.T) {
	svc := NewPTYService()
	sess, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start PTY session: %v", err)
	}

	if count := svc.ActiveSessionsCount(); count != 1 {
		t.Errorf("expected 1 active session, got %d", count)
	}

	_ = sess.Close()

	select {
	case <-sess.Done():
		// Success
	case <-time.After(1 * time.Second):
		t.Error("expected done channel to be closed")
	}

	if count := svc.ActiveSessionsCount(); count != 0 {
		t.Errorf("expected 0 active sessions after Close, got %d", count)
	}

	// Double close should not panic
	_ = sess.Close()
}

func TestPTYService_CloseAll(t *testing.T) {
	svc := NewPTYService()
	sess1, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start session 1: %v", err)
	}
	sess2, err := svc.StartSession(80, 24)
	if err != nil {
		t.Fatalf("failed to start session 2: %v", err)
	}

	if count := svc.ActiveSessionsCount(); count != 2 {
		t.Errorf("expected 2 active sessions, got %d", count)
	}

	svc.CloseAll()

	if count := svc.ActiveSessionsCount(); count != 0 {
		t.Errorf("expected 0 active sessions after CloseAll, got %d", count)
	}

	_ = sess1.Close()
	_ = sess2.Close()
}

func TestPTYService_BuildEnv(t *testing.T) {
	svc := NewPTYService()
	env := svc.buildEnv()
	var hasTerm, hasPath bool
	for _, e := range env {
		if strings.HasPrefix(e, "TERM=xterm-256color") {
			hasTerm = true
		}
		if strings.HasPrefix(e, "PATH=/opt/sbin:") {
			hasPath = true
		}
	}
	if !hasTerm {
		t.Error("expected TERM=xterm-256color in env")
	}
	if !hasPath {
		t.Error("expected PATH starting with /opt/sbin: in env")
	}
}
