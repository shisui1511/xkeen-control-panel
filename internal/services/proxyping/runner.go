package proxyping

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
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
	batch     PingBatch
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	terminal  string
	targets   map[string]Target
	excluded  map[string]bool
	completed map[string]bool
	inflight  map[string]context.CancelFunc
	results   map[string]PingResult
	// fpReady — отпечатки прочитаны после запуска пачки; до этого узлы не исключаются.
	fpReady bool
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
	fpMu     sync.Mutex
	fpAt     time.Time
	fp       map[string]string
	fpOK     bool
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
	return r.launch(req, nil)
}

// onStart передаёт Probe собственную пачку под тем же замком, что и Launch.
func (r *Runner) launch(req Request, onStart func(*batchRun)) (PingStart, error) {
	r.mu.Lock()
	if !r.started.Load() || r.ctx.Err() != nil {
		r.mu.Unlock()
		return PingStart{}, ErrStopped
	}
	if r.active != nil {
		start := PingStart{BatchID: r.active.batch.ID, Joined: true, Batch: r.active.batch}
		r.mu.Unlock()
		return start, nil
	}
	if len(req.Targets) == 0 {
		r.mu.Unlock()
		return PingStart{}, ErrNoTargets
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
	targetMap := make(map[string]Target, len(req.Targets))
	for i := range req.Targets {
		t := &req.Targets[i]
		if t.Ref == "" {
			r.mu.Unlock()
			return PingStart{}, errors.New("ping_invalid_target")
		}
		if _, exists := targetMap[t.Ref]; exists {
			r.mu.Unlock()
			return PingStart{}, errors.New("ping_duplicate_target")
		}
		if t.Fingerprint == "" {
			t.Fingerprint = Fingerprint(t.Node)
		}
		targetMap[t.Ref] = *t
	}
	ctx, cancel := context.WithCancel(r.ctx)
	b := &batchRun{batch: PingBatch{ID: id, State: StateRunning, Scope: req.Scope, Group: req.Group, Kernel: req.Kernel, Method: req.Method, Total: len(req.Targets), StartedAt: r.d.Now()}, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	b.targets, b.excluded, b.completed, b.inflight, b.results = targetMap, make(map[string]bool), make(map[string]bool), make(map[string]context.CancelFunc), make(map[string]PingResult)
	r.active = b
	if onStart != nil {
		onStart(b)
	}
	start := PingStart{BatchID: id, Batch: b.batch}
	r.wg.Add(1)
	r.mu.Unlock()
	go r.run(b, req)
	r.publish("batch")
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
		if b.terminal != "" {
			state = b.terminal
		} else if b.ctx.Err() != nil {
			state = StateCancelled
		}
		// Сбой старта из-за отмены — не причина пачки: отменённая или прерванная
		// сменой ядра пачка идёт без reason_code.
		if state == StateCancelled || state == StateKernelChanged {
			reason = ""
		}
		now := r.d.Now()
		b.batch.State, b.batch.ReasonCode, b.batch.FinishedAt = state, reason, &now
		last := b.batch
		r.last, r.active = &last, nil
		close(b.done)
		r.mu.Unlock()
		r.publish("batch")
	}()
	watchCtx, stopWatch := context.WithCancel(b.ctx)
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); r.watch(watchCtx, b, req.Kernel) }()
	defer func() { stopWatch(); <-watchDone }()
	r.syncFingerprints(true)
	l := r.limits()
	bin := ""
	if r.d.XrayBin != nil {
		bin = r.d.XrayBin()
	}
	fallback := ""
	if req.Method == MethodXray {
		if st, err := os.Stat(bin); bin == "" || err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			fallback = FallbackNoXrayBinary
		}
	}
	for from := 0; from < len(req.Targets) && b.ctx.Err() == nil; from += l.ChunkSize {
		targets := req.Targets[from:min(from+l.ChunkSize, len(req.Targets))]
		if req.Method == MethodMihomo || req.Method == MethodTLS {
			r.work(b, targets, l.Parallel, func(ctx context.Context, i int) PingResult { return r.directMeasure(ctx, req, targets[i], req.Method) })
			continue
		}
		if fallback == "" {
			if available, ok := r.d.MemAvailable(); ok && available < l.MinMemAvail {
				fallback = FallbackLowMemory
			}
		}
		if fallback != "" {
			r.mu.Lock()
			b.batch.FallbackReason = fallback
			r.mu.Unlock()
			r.work(b, targets, l.Parallel, func(ctx context.Context, i int) PingResult { return r.directMeasure(ctx, req, targets[i], MethodTLS) })
			continue
		}
		var xrayTargets, tlsTargets []Target
		for _, target := range targets {
			if target.Xray {
				xrayTargets = append(xrayTargets, target)
			} else {
				tlsTargets = append(tlsTargets, target)
			}
		}
		r.work(b, tlsTargets, l.Parallel, func(ctx context.Context, i int) PingResult {
			return r.directMeasure(ctx, req, tlsTargets[i], MethodTLS)
		})
		if len(xrayTargets) == 0 || b.ctx.Err() != nil {
			continue
		}
		release, err := r.acquireHelper(b.ctx)
		if err != nil {
			state, reason = StateFailed, ReasonStart
			return
		}
		// Проверка памяти непосредственно перед каждым стартом процесса.
		if available, ok := r.d.MemAvailable(); ok && available < l.MinMemAvail {
			release()
			fallback = FallbackLowMemory
			r.mu.Lock()
			b.batch.FallbackReason = fallback
			r.mu.Unlock()
			r.work(b, xrayTargets, l.Parallel, func(ctx context.Context, i int) PingResult {
				return r.directMeasure(ctx, req, xrayTargets[i], MethodTLS)
			})
			continue
		}
		p, err := startTempXray(b.ctx, bin, r.d.TmpRoot, xrayTargets)
		release()
		if err != nil {
			state, reason = StateFailed, ReasonStart
			return
		}
		r.work(b, xrayTargets, l.Parallel, func(ctx context.Context, i int) PingResult { return r.measure(ctx, req, p.ports[i]) })
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
				r.refreshFingerprints()
				r.mu.Lock()
				r.excludeChangedLocked(b, targets[i])
				if b.excluded[targets[i].Ref] {
					r.mu.Unlock()
					continue
				}
				ctx, cancel := context.WithCancel(b.ctx)
				b.inflight[targets[i].Ref] = cancel
				r.mu.Unlock()
				result := measure(ctx, i)
				if ctx.Err() == nil {
					r.save(b, targets[i], result)
				}
				cancel()
				r.mu.Lock()
				delete(b.inflight, targets[i].Ref)
				r.mu.Unlock()
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
	r.refreshFingerprints()
	r.mu.Lock()
	r.excludeChangedLocked(b, target)
	if b.ctx.Err() != nil || b.excluded[target.Ref] {
		r.mu.Unlock()
		return
	}
	previous := r.results[target.Ref]
	if previous.fingerprint != target.Fingerprint {
		previous = cachedResult{}
	}
	history := append(previous.result.History, PingPoint{MS: result.MS, Status: result.Status, At: result.At})
	if len(history) > 20 {
		history = append([]PingPoint(nil), history[len(history)-20:]...)
	}
	result.History = history
	r.results[target.Ref] = cachedResult{result, target.Fingerprint}
	b.results[target.Ref], b.completed[target.Ref] = result, true
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
	r.refreshFingerprints()
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

func (r *Runner) directMeasure(ctx context.Context, req Request, target Target, method string) PingResult {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout(req))
	defer cancel()
	result := PingResult{Kernel: req.Kernel, Method: method}
	start := time.Now()
	var err error
	reason := ReasonTLS
	if method == MethodMihomo {
		reason = ReasonDelay
		if r.d.Mihomo == nil {
			err = errors.New(ReasonDelay)
		} else {
			result.MS, err = r.d.Mihomo.ProxyDelay(ctx, target.MihomoName, req.URL, probeTimeout(req))
		}
	} else {
		err = r.d.TLSPing(ctx, target.Node)
		result.MS = max(1, int(time.Since(start).Milliseconds()))
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			reason = ReasonResolve
		}
	}
	if err == nil {
		result.Status = StatusOK
	} else {
		result.MS = 0
		result.Status, result.Reason = failure(err, reason)
	}
	result.At = r.d.Now()
	return result
}

// Cancel ждёт уборки, чтобы следующий Launch не пересёкся с ещё живым процессом.
func (r *Runner) Cancel() (PingBatch, error) {
	r.mu.Lock()
	b := r.active
	if b == nil {
		var last PingBatch
		if r.last != nil {
			last = *r.last
		}
		r.mu.Unlock()
		return last, nil
	}
	if b.terminal == "" {
		b.terminal = StateCancelled
	}
	b.cancel()
	r.mu.Unlock()
	<-b.done
	r.mu.Lock()
	out := b.batch
	r.mu.Unlock()
	return out, nil
}

func (r *Runner) watch(ctx context.Context, b *batchRun, kernel string) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if r.d.ActiveKernel() != kernel {
				r.mu.Lock()
				if b.terminal == "" {
					b.terminal = StateKernelChanged
				}
				b.cancel()
				r.mu.Unlock()
				return
			}
			r.refreshFingerprints()
		}
	}
}

// refreshFingerprints сериализует чтение каталога, не держит mu во время I/O
// и сохраняет новые отпечатки атомарно с удалением устаревших результатов.
func (r *Runner) refreshFingerprints() { r.syncFingerprints(false) }

// syncFingerprints с force читает каталог без двухсекундного кэша: так пачка
// начинает с отпечатков не старше себя. Пачка исключает узлы только по
// отпечаткам, чтение которых началось после её запуска (fpReady): карта,
// прочитанная до «Применить», не знает новых и изменённых узлов.
func (r *Runner) syncFingerprints(force bool) {
	if r.d.Fingerprints == nil {
		return
	}
	r.fpMu.Lock()
	defer r.fpMu.Unlock()
	if !force && time.Since(r.fpAt) < 2*time.Second {
		return
	}
	r.fpAt = time.Now()
	r.mu.Lock()
	readFor := r.active
	r.mu.Unlock()
	fp, err := r.d.Fingerprints()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fpOK = err == nil
	if err != nil {
		return
	}
	if readFor != nil && readFor == r.active {
		readFor.fpReady = true
	}
	r.fp = make(map[string]string, len(fp))
	for ref, fingerprint := range fp {
		r.fp[ref] = fingerprint
	}
	for ref, c := range r.results {
		if current, ok := r.fp[ref]; !ok || current != c.fingerprint {
			delete(r.results, ref)
		}
	}
	if b := r.active; b != nil {
		for _, target := range b.targets {
			r.excludeChangedLocked(b, target)
		}
	}
}

func (r *Runner) excludeChangedLocked(b *batchRun, target Target) {
	if !r.fpOK || !b.fpReady || b.excluded[target.Ref] {
		return
	}
	if current, ok := r.fp[target.Ref]; ok && current == target.Fingerprint {
		return
	}
	ref := target.Ref
	b.excluded[ref] = true
	b.batch.Excluded++
	delete(r.results, ref)
	delete(b.results, ref)
	if b.completed[ref] {
		b.batch.Done--
		delete(b.completed, ref)
	}
	if cancel := b.inflight[ref]; cancel != nil {
		cancel()
	}
}

// Probe использует ту же очередь, что и пользователь. После 30 секунд
// ожидания занятого раннера — только ограниченные TLS-пробы без процесса.
func (r *Runner) Probe(ctx context.Context, req Request) map[string]PingResult {
	if ctx.Err() != nil || !r.started.Load() || r.ctx.Err() != nil || len(req.Targets) == 0 {
		return nil
	}
	req.Scope = ScopeRepick
	wait := time.NewTimer(30 * time.Second)
	defer wait.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.ctx.Done():
			return nil
		case <-wait.C:
			return r.probeTLS(ctx, req)
		default:
		}
		var own *batchRun
		_, err := r.launch(req, func(b *batchRun) { own = b })
		if err != nil {
			return nil
		}
		if own != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-r.ctx.Done():
				return nil
			case <-own.done:
				r.mu.Lock()
				state := own.batch.State
				out := make(map[string]PingResult, len(own.results))
				for ref, result := range own.results {
					result.History = append([]PingPoint(nil), result.History[max(0, len(result.History)-10):]...)
					out[ref] = result
				}
				excluded := make(map[string]bool, len(own.excluded))
				for ref := range own.excluded {
					excluded[ref] = true
				}
				r.mu.Unlock()
				if state == StateDone {
					return out
				}
				// Пачка не дошла до конца (временный Xray не запустился, ядро
				// сменилось, пачку отменили): неизмеренные узлы — не «мёртвые»,
				// их проверяет TLS, как до общего раннера. Исключённые из пачки
				// (узел изменился или пропал) не проверяются.
				return r.probeMissing(ctx, req, out, excluded)
			}
		}
		r.mu.Lock()
		var done <-chan struct{}
		if r.active != nil {
			done = r.active.done
		}
		r.mu.Unlock()
		if done == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-r.ctx.Done():
			return nil
		case <-done:
		case <-wait.C:
			return r.probeTLS(ctx, req)
		}
	}
}

// probeMissing дополняет результаты незавершённой пачки TLS-пробами узлов,
// которые она не успела измерить. Раннер остановлен — nil, как у Probe.
func (r *Runner) probeMissing(ctx context.Context, req Request, out map[string]PingResult, excluded map[string]bool) map[string]PingResult {
	var missing []Target
	for _, target := range req.Targets {
		if _, ok := out[target.Ref]; !ok && !excluded[target.Ref] {
			missing = append(missing, target)
		}
	}
	if len(missing) == 0 {
		return out
	}
	req.Targets = missing
	extra := r.probeTLS(ctx, req)
	if extra == nil {
		return nil
	}
	for ref, result := range extra {
		out[ref] = result
	}
	return out
}

func (r *Runner) probeTLS(ctx context.Context, req Request) map[string]PingResult {
	r.mu.Lock()
	if r.ctx.Err() != nil {
		r.mu.Unlock()
		return nil
	}
	r.wg.Add(1)
	r.mu.Unlock()
	defer r.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	if req.Kernel == "" {
		req.Kernel = r.d.ActiveKernel()
	}
	out := make(map[string]PingResult)
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan Target)
	for range min(r.limits().Parallel, len(req.Targets)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				if ctx.Err() != nil {
					return
				}
				result := r.directMeasure(ctx, req, target, MethodTLS)
				if ctx.Err() == nil {
					mu.Lock()
					out[target.Ref] = result
					mu.Unlock()
				}
			}
		}()
	}
dispatch:
	for _, target := range req.Targets {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- target:
		}
	}
	close(jobs)
	wg.Wait()
	return out
}
