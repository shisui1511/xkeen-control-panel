package proxyping

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

type DelayAPI interface {
	ProxyDelay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error)
}

type Deps struct {
	Publish      func(kind string)
	ActiveKernel func() string
	Mihomo       DelayAPI
	TLSPing      func(ctx context.Context, n nodes.Node) error
	XrayBin      func() string
	Fingerprints func() (map[string]string, error)
	Limits       func() Limits
	MemAvailable func() (uint64, bool)
	TmpRoot      string
	KernelHelper func() (release func(), ok bool)
	Now          func() time.Time
}

type cachedResult struct {
	result      PingResult
	fingerprint string
}

type batchRun struct {
	batch  PingBatch
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// Runner создаётся в main; Start не запускает замеров. Сетевую работу
// инициируют только Launch и Probe, независимо от жизни HTTP-запроса.
type Runner struct {
	d        Deps
	ctx      context.Context
	cancel   context.CancelFunc
	started  atomic.Bool
	mu       sync.Mutex
	wg       sync.WaitGroup
	active   *batchRun
	last     *PingBatch
	results  map[string]cachedResult
	lastPing time.Time
}

func New(d Deps) (*Runner, error) {
	if d.ActiveKernel == nil || d.TLSPing == nil {
		return nil, errors.New("ping_missing_dependency")
	}
	if d.TmpRoot == "" {
		d.TmpRoot = DefaultTmpRoot
	}
	if d.Limits == nil {
		d.Limits = func() Limits { return DefaultLimits(runtime.GOARCH, runtime.NumCPU()) }
	}
	if d.MemAvailable == nil {
		d.MemAvailable = MemAvailable
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{d: d, ctx: ctx, cancel: cancel, results: make(map[string]cachedResult)}, nil
}

func (r *Runner) Start() { r.started.CompareAndSwap(false, true) }

func (r *Runner) Stop() {
	r.mu.Lock()
	r.cancel()
	if r.active != nil {
		r.active.cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Runner) Launch(req Request) (PingStart, error) {
	r.mu.Lock()
	if !r.started.Load() || r.ctx.Err() != nil {
		r.mu.Unlock()
		return PingStart{}, ErrStopped
	}
	if len(req.Targets) == 0 {
		r.mu.Unlock()
		return PingStart{}, ErrNoTargets
	}
	if r.active != nil {
		r.mu.Unlock()
		return PingStart{}, errors.New("ping_busy")
	}
	if req.Kernel == "" {
		req.Kernel = r.d.ActiveKernel()
	}
	if req.Method == "" {
		req.Method = MethodXray
	}
	if req.Scope == "" {
		req.Scope = ScopeAll
	}
	if req.Method != MethodXray && req.Method != MethodMihomo && req.Method != MethodTLS {
		r.mu.Unlock()
		return PingStart{}, errors.New("ping_invalid_method")
	}
	id, err := randomID()
	if err != nil {
		r.mu.Unlock()
		return PingStart{}, errors.New(ReasonStart)
	}
	req.Targets = append([]Target(nil), req.Targets...)
	ctx, cancel := context.WithCancel(r.ctx)
	b := &batchRun{batch: PingBatch{ID: id, State: StateRunning, Scope: req.Scope, Group: req.Group, Kernel: req.Kernel, Method: req.Method, Total: len(req.Targets), StartedAt: r.d.Now()}, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	r.active = b
	start := PingStart{BatchID: id, Batch: b.batch}
	r.wg.Add(1)
	r.mu.Unlock()
	r.publish("batch")
	go r.run(b, req)
	return start, nil
}

func (r *Runner) publish(kind string) {
	if r.d.Publish != nil {
		r.d.Publish(kind)
	}
}

func (r *Runner) limits() Limits {
	l := r.d.Limits()
	if l.Parallel < 1 {
		l.Parallel = 1
	}
	if l.Parallel > 4 {
		l.Parallel = 4
	}
	if l.ChunkSize < 1 || l.ChunkSize > 64 {
		l.ChunkSize = 64
	}
	return l
}

func (r *Runner) run(b *batchRun, req Request) {
	defer r.wg.Done()
	defer b.cancel()
	state, reason := StateDone, ""
	defer func() {
		r.mu.Lock()
		if b.ctx.Err() != nil {
			state = StateCancelled
		}
		now := r.d.Now()
		b.batch.State, b.batch.ReasonCode, b.batch.FinishedAt = state, reason, &now
		last := b.batch
		r.last, r.active = &last, nil
		close(b.done)
		r.mu.Unlock()
		r.publish("batch")
	}()
	l := r.limits()
	bin := ""
	if r.d.XrayBin != nil {
		bin = r.d.XrayBin()
	}
	for from := 0; from < len(req.Targets) && b.ctx.Err() == nil; from += l.ChunkSize {
		targets := req.Targets[from:min(from+l.ChunkSize, len(req.Targets))]
		release, err := r.acquireHelper(b.ctx)
		if err != nil {
			state, reason = StateFailed, ReasonStart
			return
		}
		p, err := startTempXray(b.ctx, bin, r.d.TmpRoot, targets)
		release()
		if err != nil {
			state, reason = StateFailed, ReasonStart
			return
		}
		r.work(b, targets, l.Parallel, func(ctx context.Context, i int) PingResult { return r.measure(ctx, req, p.ports[i]) })
		p.stop()
	}
}

func (r *Runner) acquireHelper(ctx context.Context) (func(), error) {
	if r.d.KernelHelper == nil {
		return func() {}, nil
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if wait.Err() != nil {
			return nil, wait.Err()
		}
		if release, ok := r.d.KernelHelper(); ok {
			return release, nil
		}
		select {
		case <-wait.Done():
			return nil, wait.Err()
		case <-t.C:
		}
	}
}

func (r *Runner) work(b *batchRun, targets []Target, parallel int, measure func(context.Context, int) PingResult) {
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range min(parallel, len(targets)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if b.ctx.Err() != nil {
					return
				}
				result := measure(b.ctx, i)
				if b.ctx.Err() == nil {
					r.save(b, targets[i], result)
				}
			}
		}()
	}
dispatch:
	for i := range targets {
		select {
		case <-b.ctx.Done():
			break dispatch
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}

func probeTimeout(req Request) time.Duration { return max(5*time.Second, req.Timeout) }

func failure(err error, reason string) (string, string) {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return StatusTimeout, ReasonTimeout
	}
	return StatusError, reason
}

func (r *Runner) measure(ctx context.Context, req Request, port int) PingResult {
	proxyURL := &url.URL{Scheme: "socks5h", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: probeTimeout(req)}
	result := PingResult{Kernel: req.Kernel, Method: MethodXray, Status: StatusError, Reason: ReasonTunnel}
	var sum, good int
	for i := 0; i < 3 && ctx.Err() == nil; i++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
		if err != nil {
			break
		}
		start := time.Now()
		resp, err := client.Do(request)
		if err != nil {
			result.Status, result.Reason = failure(err, ReasonTunnel)
			continue
		}
		_ = resp.Body.Close()
		if i > 0 {
			sum += max(1, int(time.Since(start).Milliseconds()))
			good++
		}
	}
	if good > 0 {
		result.MS, result.Status, result.Reason = sum/good, StatusOK, ""
	}
	result.At = r.d.Now()
	return result
}

func (r *Runner) save(b *batchRun, target Target, result PingResult) {
	r.mu.Lock()
	history := append(r.results[target.Ref].result.History, PingPoint{MS: result.MS, Status: result.Status, At: result.At})
	if len(history) > 20 {
		history = append([]PingPoint(nil), history[len(history)-20:]...)
	}
	result.History = history
	r.results[target.Ref] = cachedResult{result, target.Fingerprint}
	b.batch.Done++
	now := time.Now()
	notify := now.Sub(r.lastPing) >= 500*time.Millisecond
	if notify {
		r.lastPing = now
	}
	r.mu.Unlock()
	if notify {
		r.publish("ping")
	}
}

func (r *Runner) Snapshot() PingSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := PingSnapshot{Results: make(map[string]PingResult, len(r.results))}
	if r.active != nil {
		b := r.active.batch
		out.Batch = &b
	} else if r.last != nil {
		b := *r.last
		out.Batch = &b
	}
	for ref, c := range r.results {
		result := c.result
		result.History = append([]PingPoint(nil), result.History[max(0, len(result.History)-10):]...)
		out.Results[ref] = result
	}
	return out
}
