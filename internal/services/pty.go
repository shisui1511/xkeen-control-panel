package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	// MaxPTYSessions defines the maximum number of concurrent interactive PTY sessions
	MaxPTYSessions = 2
)

var (
	// ErrMaxSessionsReached is returned when trying to start more sessions than allowed
	ErrMaxSessionsReached = errors.New("maximum active terminal sessions reached")
)

// PTYSession represents an active interactive terminal session
type PTYSession struct {
	ID        string
	cmd       *exec.Cmd
	ptmx      *os.File
	closed    bool
	closeOnce sync.Once
	done      chan struct{}
	service   *PTYService
	// exited закрывается после Wait процесса; exitCode читается только после него
	exited   chan struct{}
	exitCode int
	// drained закрывается, когда Read вернул ошибку (вывод дочитан до EIO)
	drained     chan struct{}
	drainedOnce sync.Once
}

// PTYService manages interactive PTY sessions
type PTYService struct {
	mu          sync.Mutex
	sessions    map[string]*PTYSession
	maxSessions int
}

// NewPTYService initializes a new PTY service
func NewPTYService() *PTYService {
	return &PTYService{
		sessions:    make(map[string]*PTYSession),
		maxSessions: MaxPTYSessions,
	}
}

// detectShell finds the best available shell on the system
func (s *PTYService) detectShell() string {
	candidates := []string{
		"/opt/bin/bash",
		"/opt/bin/sh",
		"/opt/bin/ash",
		"/bin/bash",
		"/bin/sh",
		"/bin/ash",
	}

	for _, path := range candidates {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	return "/bin/sh"
}

// buildEnv builds the environment variables for the PTY session
func (s *PTYService) buildEnv() []string {
	envMap := make(map[string]string)

	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Preset standard terminal & Entware environment
	envMap["TERM"] = "xterm-256color"
	envMap["HOME"] = "/root"
	if envMap["LANG"] == "" {
		envMap["LANG"] = "en_US.UTF-8"
	}
	if envMap["LC_ALL"] == "" {
		envMap["LC_ALL"] = "en_US.UTF-8"
	}
	envMap["PATH"] = "/opt/sbin:/opt/bin:/opt/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	return env
}

// clampWinsize приводит размер окна к допустимому диапазону.
func clampWinsize(cols, rows int) *pty.Winsize {
	var winCols uint16 = 80
	if cols >= 1 && cols <= 1000 {
		winCols = uint16(cols)
	} else if cols > 1000 {
		winCols = 1000
	}

	var winRows uint16 = 24
	if rows >= 1 && rows <= 500 {
		winRows = uint16(rows)
	} else if rows > 500 {
		winRows = 500
	}
	return &pty.Winsize{Rows: winRows, Cols: winCols}
}

// StartSession initiates a new PTY process with the requested window size
func (s *PTYService) StartSession(cols, rows int) (*PTYSession, error) {
	shellPath := s.detectShell()
	if shellPath != "/bin/sh" {
		return s.StartCommand(cols, rows, []string{shellPath, "-l"})
	}
	return s.StartCommand(cols, rows, []string{shellPath})
}

// StartCommand запускает argv в новой PTY-сессии (вместо интерактивной
// оболочки) — например, интерактивный установщик.
func (s *PTYService) StartCommand(cols, rows int, argv []string) (*PTYSession, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	s.mu.Lock()
	if len(s.sessions) >= s.maxSessions {
		s.mu.Unlock()
		return nil, ErrMaxSessionsReached
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = s.buildEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	ptmx, err := pty.StartWithSize(cmd, clampWinsize(cols, rows))
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	sess := &PTYSession{
		ID:      id,
		cmd:     cmd,
		ptmx:    ptmx,
		done:    make(chan struct{}),
		service: s,
		exited:  make(chan struct{}),
		drained: make(chan struct{}),
	}

	s.sessions[id] = sess
	s.mu.Unlock()

	// Background reaper goroutine to prevent zombie processes
	go func() {
		_ = cmd.Wait()
		sess.exitCode = cmd.ProcessState.ExitCode()
		close(sess.exited)
		// Дать читателю забрать остаток вывода: закрытый раньше времени ptmx
		// теряет последние строки и сообщение о завершении
		select {
		case <-sess.drained:
		case <-time.After(ptyDrainTimeout):
		}
		_ = sess.Close()
	}()

	return sess, nil
}

// ExitCode ждёт завершения процесса сессии (не дольше wait) и возвращает его
// код; -1 — процесс ещё работает или убит сигналом.
func (s *PTYSession) ExitCode(wait time.Duration) int {
	select {
	case <-s.exited:
		return s.exitCode
	case <-time.After(wait):
		return -1
	}
}

// ptyDrainTimeout — сколько после выхода процесса ждать, пока читатель
// дочитает вывод, прежде чем закрыть PTY.
const ptyDrainTimeout = 2 * time.Second

// Read reads output from the PTY master device
func (s *PTYSession) Read(p []byte) (n int, err error) {
	n, err = s.ptmx.Read(p)
	if err != nil {
		s.drainedOnce.Do(func() { close(s.drained) })
	}
	return n, err
}

// Write writes input to the PTY master device
func (s *PTYSession) Write(p []byte) (n int, err error) {
	return s.ptmx.Write(p)
}

// Resize updates the terminal geometry
func (s *PTYSession) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("invalid terminal dimensions")
	}

	s.service.mu.Lock()
	defer s.service.mu.Unlock()

	if s.closed || s.ptmx == nil {
		return errors.New("terminal session closed")
	}

	return pty.Setsize(s.ptmx, clampWinsize(cols, rows))
}

// Done returns a channel that is closed when the session terminates
func (s *PTYSession) Done() <-chan struct{} {
	return s.done
}

// Close gracefully terminates the PTY session and kills the underlying process
func (s *PTYSession) Close() error {
	s.closeOnce.Do(func() {
		s.service.mu.Lock()
		s.closed = true
		delete(s.service.sessions, s.ID)
		s.service.mu.Unlock()

		close(s.done)

		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}

		if s.cmd != nil && s.cmd.Process != nil {
			// Try process group kill, then fallback to direct process kill
			pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
			if err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			} else {
				_ = s.cmd.Process.Signal(syscall.SIGTERM)
			}
			_ = s.cmd.Process.Kill()
		}
	})
	return nil
}

// ActiveSessionsCount returns the number of currently active PTY sessions
func (s *PTYService) ActiveSessionsCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// CloseAll terminates all active PTY sessions
func (s *PTYService) CloseAll() {
	s.mu.Lock()
	all := make([]*PTYSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	s.mu.Unlock()

	for _, sess := range all {
		_ = sess.Close()
	}
}
