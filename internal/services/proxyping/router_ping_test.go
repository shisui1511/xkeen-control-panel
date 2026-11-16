//go:build router

// router-area: routing

package proxyping

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Новый механизм: изолированный Xray и реальные loopback-туннели на устройстве.
// Рабочее ядро не читается и не переключается, поэтому метка ядра не нужна.
func TestRouterPingTempXray(t *testing.T) {
	routertest.Slow(t, "пачки временного Xray на реальном времени: отмена зависших узлов и замер памяти на 48 узлах")
	if _, err := os.Stat("/opt/sbin/xray"); err != nil {
		t.Fatal("xray_binary_missing")
	}

	t.Run("temp_process_name_and_cleanup", func(t *testing.T) {
		url := pingHTTP(t, 100*time.Millisecond)
		proxy := pingSOCKS(t, false)
		dead := pingDeadPort(t)
		targets := pingTargets(proxy.port, 2)
		targets = append(targets, pingTargets(dead, 1)...)
		targets[2].Ref = "rt/2"
		r, _ := pingRunner(t, nil)
		pingLaunch(t, r, targets, url)
		pid, dir := pingWaitProcess(t, r.d.TmpRoot)
		exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
		if err != nil || filepath.Base(exe) != ProcName {
			t.Fatal("process_exe_name")
		}
		comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		if err != nil || strings.TrimSpace(string(comm)) != ProcName {
			t.Fatal("process_comm_name")
		}
		out, err := exec.Command("pidof", "xray").Output()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			t.Fatal("pidof_failed")
		}
		for _, found := range strings.Fields(string(out)) {
			if found == strconv.Itoa(pid) {
				t.Fatal("process_detected_as_working_core")
			}
		}
		for _, path := range []string{dir, filepath.Join(dir, "00_base.json"), filepath.Join(dir, "10_probe.json"), filepath.Join(dir, "pid"), filepath.Join(dir, "out.log")} {
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal("private_file_missing")
			}
			mode := os.FileMode(0o600)
			if path == dir {
				mode = 0o700
			}
			if st.Mode().Perm() != mode {
				t.Fatal("private_file_mode")
			}
		}
		s := pingWaitBatch(t, r, StateDone, 20*time.Second)
		for i := 0; i < 2; i++ {
			v := s.Results[targets[i].Ref]
			if v.Status != StatusOK || v.MS <= 0 || v.Method != MethodXray {
				t.Fatalf("live_result_%d: status=%s reason=%s", i, v.Status, v.Reason)
			}
		}
		if v := s.Results[targets[2].Ref]; v.Status != StatusError {
			t.Fatalf("dead_result: status=%s reason=%s", v.Status, v.Reason)
		}
		pingGone(t, pid, dir)
	})

	t.Run("parallel_limit", func(t *testing.T) {
		proxy := pingSOCKS(t, false)
		r, _ := pingRunner(t, nil)
		pingLaunch(t, r, pingTargets(proxy.port, 8), pingHTTP(t, 300*time.Millisecond))
		pingWaitBatch(t, r, StateDone, 20*time.Second)
		if n := proxy.peak.Load(); n < 1 || n > 2 {
			t.Fatalf("parallel_peak=%d", n)
		}
	})

	t.Run("join_and_cancel", func(t *testing.T) {
		live, hung := pingSOCKS(t, false), pingSOCKS(t, true)
		targets := pingTargets(hung.port, 4)
		targets[0] = pingTargets(live.port, 1)[0]
		r, _ := pingRunner(t, nil)
		first := pingLaunch(t, r, targets, pingHTTP(t, 0))
		pid, dir := pingWaitProcess(t, r.d.TmpRoot)
		joined, err := r.Launch(Request{Scope: ScopeAll})
		if err != nil || !joined.Joined || joined.BatchID != first.BatchID {
			t.Fatal("batch_not_joined")
		}
		if _, err := r.Launch(Request{Scope: ScopeGroup, Group: "other"}); !errors.Is(err, ErrBusy) {
			t.Fatalf("foreign_batch_not_busy: %v", err)
		}
		routertest.WaitCond(t, 5*time.Second, "готовый узел", func() bool { return r.Snapshot().Results[targets[0].Ref].Status == StatusOK })
		start := time.Now()
		batch, err := r.Cancel()
		if err != nil || batch.State != StateCancelled || batch.Done >= batch.Total || time.Since(start) > 5*time.Second {
			t.Fatalf("cancel: state=%s done=%d total=%d", batch.State, batch.Done, batch.Total)
		}
		if r.Snapshot().Results[targets[0].Ref].Status != StatusOK {
			t.Fatal("ready_result_lost")
		}
		pingGone(t, pid, dir)
	})

	t.Run("kernel_changed", func(t *testing.T) {
		live, hung := pingSOCKS(t, false), pingSOCKS(t, true)
		targets := pingTargets(hung.port, 3)
		targets[0] = pingTargets(live.port, 1)[0]
		r, kernel := pingRunner(t, nil)
		pingLaunch(t, r, targets, pingHTTP(t, 0))
		pid, dir := pingWaitProcess(t, r.d.TmpRoot)
		routertest.WaitCond(t, 5*time.Second, "готовый узел", func() bool { return r.Snapshot().Results[targets[0].Ref].Status == StatusOK })
		kernel.Store("mihomo")
		s := pingWaitBatch(t, r, StateKernelChanged, 3*time.Second)
		if len(s.Results) == 0 {
			t.Fatal("ready_result_lost")
		}
		for _, v := range s.Results {
			if v.Kernel != "xray" {
				t.Fatal("result_kernel_changed")
			}
		}
		pingGone(t, pid, dir)
	})

	t.Run("fingerprint_changed", func(t *testing.T) {
		proxy := pingSOCKS(t, false)
		targets := pingTargets(proxy.port, 5)
		var fingerprints atomic.Value
		before := make(map[string]string)
		for _, target := range targets {
			before[target.Ref] = target.Fingerprint
		}
		fingerprints.Store(before)
		r, _ := pingRunner(t, func(d *Deps) {
			d.Limits = func() Limits { return Limits{1, 1, 64, "test"} }
			d.Fingerprints = func() (map[string]string, error) { return fingerprints.Load().(map[string]string), nil }
		})
		pingLaunch(t, r, targets, pingHTTP(t, 400*time.Millisecond))
		routertest.WaitCond(t, 4*time.Second, "начало замера", func() bool { return proxy.total.Load() > 0 })
		after := make(map[string]string)
		for ref, fp := range before {
			after[ref] = fp
		}
		changed := targets[4].Node
		changed.Port++
		after[targets[4].Ref] = Fingerprint(changed)
		fingerprints.Store(after)
		s := pingWaitBatch(t, r, StateDone, 15*time.Second)
		if s.Batch.Excluded != 1 || s.Batch.Done != 4 {
			t.Fatalf("fingerprints: excluded=%d done=%d", s.Batch.Excluded, s.Batch.Done)
		}
		if _, exists := s.Results[targets[4].Ref]; exists {
			t.Fatal("changed_result_saved")
		}
	})

	// WR-02: карта отпечатков, прочитанная до запуска пачки (кэш 2 с), не знает нового узла;
	// пачка читает каталог заново и не исключает такой узел.
	t.Run("fingerprint_stale_before_batch", func(t *testing.T) {
		proxy := pingSOCKS(t, false)
		targets := pingTargets(proxy.port, 3)
		full := make(map[string]string, len(targets))
		stale := make(map[string]string, len(targets))
		for i, target := range targets {
			full[target.Ref] = target.Fingerprint
			if i < 2 {
				stale[target.Ref] = target.Fingerprint
			}
		}
		var fingerprints atomic.Value
		fingerprints.Store(stale)
		r, _ := pingRunner(t, func(d *Deps) {
			d.Fingerprints = func() (map[string]string, error) { return fingerprints.Load().(map[string]string), nil }
		})
		r.Snapshot()
		fingerprints.Store(full)
		pingLaunch(t, r, targets, pingHTTP(t, 0))
		s := pingWaitBatch(t, r, StateDone, 15*time.Second)
		if s.Batch.Excluded != 0 || s.Batch.Done != len(targets) {
			t.Fatalf("stale_fingerprints: excluded=%d done=%d", s.Batch.Excluded, s.Batch.Done)
		}
		if _, ok := s.Results[targets[2].Ref]; !ok {
			t.Fatal("new_node_not_measured")
		}
	})

	// WR-01: пачка повторного выбора не дошла до конца (временный Xray не запустился) —
	// неизмеренные узлы проверяет TLS, а не считает мёртвыми.
	t.Run("probe_failed_batch_tls", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		t.Cleanup(server.Close)
		_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
		n, _ := strconv.Atoi(port)
		targets := pingTargets(n, 2)
		fakeDir, err := os.MkdirTemp("/opt/tmp", "xcp-ping-fake-")
		if err != nil {
			t.Fatal("fake_dir")
		}
		t.Cleanup(func() { _ = os.RemoveAll(fakeDir) })
		fake := filepath.Join(fakeDir, "xray")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal("fake_xray")
		}
		r, _ := pingRunner(t, func(d *Deps) { d.XrayBin = func() string { return fake } })
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		got := r.Probe(ctx, Request{Kernel: "xray", Method: MethodXray, Targets: targets, URL: server.URL})
		if last := r.Snapshot().Batch; last == nil || last.State != StateFailed || last.ReasonCode != ReasonStart {
			t.Fatalf("batch_not_failed: %+v", last)
		}
		for _, target := range targets {
			if res, ok := got[target.Ref]; !ok || res.Method != MethodTLS || res.Status != StatusOK {
				t.Fatalf("not_tls_checked: %s %+v", target.Ref, res)
			}
		}
	})

	t.Run("tls_fallback", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		t.Cleanup(server.Close)
		_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
		n, _ := strconv.Atoi(port)
		targets := pingTargets(n, 1)
		for _, reason := range []string{FallbackNoXrayBinary, FallbackLowMemory} {
			t.Run(reason, func(t *testing.T) {
				r, _ := pingRunner(t, func(d *Deps) {
					if reason == FallbackNoXrayBinary {
						d.XrayBin = func() string { return "/opt/tmp/xcp-ping-absent/xray" }
					} else {
						d.Limits = func() Limits { return Limits{2, 64 << 20, 64, "test"} }
						d.MemAvailable = func() (uint64, bool) { return 1 << 20, true }
					}
				})
				pingLaunch(t, r, targets, server.URL)
				s := pingWaitBatch(t, r, StateDone, 5*time.Second)
				if s.Batch.FallbackReason != reason || s.Results[targets[0].Ref].Method != MethodTLS || s.Results[targets[0].Ref].Status != StatusOK {
					t.Fatalf("fallback: reason=%s method=%s status=%s", s.Batch.FallbackReason, s.Results[targets[0].Ref].Method, s.Results[targets[0].Ref].Status)
				}
			})
		}
	})

	t.Run("sweep_stale", func(t *testing.T) {
		r, _ := pingRunner(t, nil)
		proxy := pingSOCKS(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		p, err := startTempXray(ctx, "/opt/sbin/xray", r.d.TmpRoot, pingTargets(proxy.port, 1))
		if err != nil {
			t.Fatal("stale_process_start")
		}
		t.Cleanup(p.stop)
		other := exec.Command("sleep", "30")
		if err := other.Start(); err != nil {
			t.Fatal("other_process_start")
		}
		t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
		id, err := randomID()
		if err != nil {
			t.Fatal("random_id")
		}
		dir := filepath.Join(r.d.TmpRoot, id)
		if os.Mkdir(dir, 0o700) != nil || privateFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(other.Process.Pid)), 0o600) != nil {
			t.Fatal("foreign_pid_file")
		}
		if err := r.SweepStale(); err != nil {
			t.Fatal("sweep_failed")
		}
		routertest.WaitCond(t, 3*time.Second, "выход оставленного процесса", func() bool {
			select {
			case <-p.done:
				return true
			default:
				return false
			}
		})
		pingGone(t, p.cmd.Process.Pid, p.dir)
		if other.Process.Signal(syscall.Signal(0)) != nil {
			t.Fatal("foreign_process_killed")
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("foreign_directory_remains")
		}
	})

	t.Run("memory_probe", func(t *testing.T) {
		proxy := pingSOCKS(t, false)
		url := pingHTTP(t, 30*time.Millisecond)
		for _, count := range []int{14, 48} {
			t.Run(strconv.Itoa(count), func(t *testing.T) {
				r, _ := pingRunner(t, func(d *Deps) { d.Limits = func() Limits { return Limits{4, 1, 64, "test"} } })
				base, ok := MemAvailable()
				if !ok {
					t.Fatal("memavailable_missing")
				}
				minMem, peak := base, uint64(0)
				start := time.Now()
				pingLaunch(t, r, pingTargets(proxy.port, count), url)
				ok, state := routertest.Poll(20*time.Second, 200*time.Millisecond, func() (bool, string) {
					if mem, ok := MemAvailable(); ok && mem < minMem {
						minMem = mem
					}
					pid, _ := pingProcess(r.d.TmpRoot)
					if v := pingHWM(pid); v > peak {
						peak = v
					}
					s := r.Snapshot()
					return s.Batch != nil && s.Batch.State != StateRunning, s.Batch.State
				})
				t.Logf("ping-mem: nodes=%d vmhwm_kb=%d memavail_drop_kb=%d dur=%s", count, peak, (base-minMem)/1024, time.Since(start).Round(time.Millisecond))
				if !ok || state != StateDone {
					t.Fatalf("memory_batch: state=%s", state)
				}
			})
		}
	})
}

func pingRunner(t *testing.T, edit func(*Deps)) (*Runner, *atomic.Value) {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal("random_id")
	}
	root := "/opt/tmp/xcp-ping-rt-" + id
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal("test_root_create")
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	kernel := &atomic.Value{}
	kernel.Store("xray")
	d := Deps{TmpRoot: root, ActiveKernel: func() string { return kernel.Load().(string) }, XrayBin: func() string { return "/opt/sbin/xray" }, Limits: func() Limits { return Limits{2, 1, 64, "test"} }, TLSPing: func(ctx context.Context, n nodes.Node) error {
		// Самоподписанный сертификат — только у loopback-сервера этого теста.
		if ip := net.ParseIP(n.Host); ip == nil || !ip.IsLoopback() {
			return errors.New("loopback_required")
		}
		dialer := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(n.Host, strconv.Itoa(n.Port)))
		if conn != nil {
			_ = conn.Close()
		}
		return err
	}}
	if edit != nil {
		edit(&d)
	}
	r, err := New(d)
	if err != nil {
		t.Fatal("runner_create")
	}
	r.Start()
	t.Cleanup(r.Stop)
	return r, kernel
}

func pingTargets(port, count int) []Target {
	out := make([]Target, count)
	for i := range out {
		n := nodes.Node{Protocol: "socks", Host: "127.0.0.1", Port: port}
		out[i] = Target{Ref: "rt/" + strconv.Itoa(i), Node: n, Xray: true, Fingerprint: Fingerprint(n)}
	}
	return out
}

func pingLaunch(t *testing.T, r *Runner, targets []Target, url string) PingStart {
	t.Helper()
	start, err := r.Launch(Request{Scope: ScopeAll, Kernel: "xray", Method: MethodXray, Targets: targets, URL: url})
	if err != nil {
		t.Fatal("batch_launch")
	}
	return start
}

func pingWaitBatch(t *testing.T, r *Runner, state string, limit time.Duration) PingSnapshot {
	t.Helper()
	var snapshot PingSnapshot
	ok, seen := routertest.Poll(limit, 25*time.Millisecond, func() (bool, string) {
		snapshot = r.Snapshot()
		if snapshot.Batch == nil {
			return false, "missing"
		}
		return snapshot.Batch.State != StateRunning, snapshot.Batch.State
	})
	if !ok || seen != state {
		t.Fatalf("batch_state=%s expected=%s", seen, state)
	}
	return snapshot
}

func pingHTTP(t *testing.T, delay time.Duration) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-req.Context().Done():
			return
		case <-timer.C:
		}
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/generate_204"
}

func pingDeadPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("dead_port_allocate")
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func pingProcess(root string) (int, string) {
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "pid"))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(data))
		if err == nil && ownsProcess(pid, dir) {
			return pid, dir
		}
	}
	return 0, ""
}

func pingWaitProcess(t *testing.T, root string) (int, string) {
	t.Helper()
	var pid int
	var dir string
	routertest.WaitCond(t, 12*time.Second, "временный процесс", func() bool { pid, dir = pingProcess(root); return pid > 0 })
	return pid, dir
}

func pingGone(t *testing.T, pid int, dir string) {
	t.Helper()
	routertest.WaitCond(t, 3*time.Second, "уборка процесса и каталога", func() bool {
		_, procErr := os.Stat("/proc/" + strconv.Itoa(pid))
		_, dirErr := os.Stat(dir)
		return errors.Is(procErr, os.ErrNotExist) && errors.Is(dirErr, os.ErrNotExist)
	})
}

func pingHWM(pid int) uint64 {
	if pid == 0 {
		return 0
	}
	data, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmHWM:" {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			return value
		}
	}
	return 0
}

type pingProxy struct {
	port    int
	total   atomic.Int32
	current atomic.Int32
	peak    atomic.Int32
}

// pingSOCKS — настоящий SOCKS5-сервер, который соединяет только loopback-цели.
// В зависшем режиме соединение остаётся открытым до уборки окружения теста.
func pingSOCKS(t *testing.T, hung bool) *pingProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("proxy_listen")
	}
	p := &pingProxy{port: l.Addr().(*net.TCPAddr).Port}
	var mu sync.Mutex
	conns := make(map[net.Conn]bool)
	var wg sync.WaitGroup
	done, stop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns[conn] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { _ = conn.Close(); mu.Lock(); delete(conns, conn); mu.Unlock() }()
				p.total.Add(1)
				if hung {
					<-stop
					return
				}
				var header [2]byte
				if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 {
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				if _, err := conn.Write([]byte{5, 0}); err != nil {
					return
				}
				var request [4]byte
				if _, err := io.ReadFull(conn, request[:]); err != nil || request[1] != 1 {
					return
				}
				var host string
				switch request[3] {
				case 1:
					b := make([]byte, 4)
					if _, err := io.ReadFull(conn, b); err != nil {
						return
					}
					host = net.IP(b).String()
				case 4:
					b := make([]byte, 16)
					if _, err := io.ReadFull(conn, b); err != nil {
						return
					}
					host = net.IP(b).String()
				case 3:
					var length [1]byte
					if _, err := io.ReadFull(conn, length[:]); err != nil {
						return
					}
					b := make([]byte, int(length[0]))
					if _, err := io.ReadFull(conn, b); err != nil {
						return
					}
					host = string(b)
				default:
					return
				}
				var port [2]byte
				if _, err := io.ReadFull(conn, port[:]); err != nil {
					return
				}
				if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
					return
				}
				up, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port[:])))), time.Second)
				if err != nil {
					return
				}
				defer up.Close()
				current := p.current.Add(1)
				for old := p.peak.Load(); current > old && !p.peak.CompareAndSwap(old, current); old = p.peak.Load() {
				}
				counted := true
				defer func() {
					if counted {
						p.current.Add(-1)
					}
				}()
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				copied := make(chan struct{})
				go func() { _, _ = io.Copy(up, conn); close(copied) }()
				// Проба отвечает с Connection: close. Сначала закрываем соединение
				// с целью, затем отдаём ответ клиенту: счётчик измеряет настоящие
				// открытые туннели, без хвоста планирования после ответа клиенту.
				response, readErr := io.ReadAll(up)
				_ = up.Close()
				p.current.Add(-1)
				counted = false
				if readErr == nil {
					_, _ = conn.Write(response)
				}
				_ = conn.Close()
				_ = up.Close()
				<-copied
			}()
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = l.Close()
		<-done
		mu.Lock()
		for conn := range conns {
			_ = conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return p
}
