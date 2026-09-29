package services

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const (
	// XKeenStatusPollInterval — пауза между опросами `xkeen -status`, отсчитывается
	// от ЗАВЕРШЕНИЯ предыдущего опроса, а не от его начала.
	XKeenStatusPollInterval = 10 * time.Second
	// xkeenStatusPollTimeout — таймаут одного `xkeen -status` в цикле кэша
	// (под нагрузкой роутера скрипт отвечает медленно).
	xkeenStatusPollTimeout = 10 * time.Second
	// xkeenVersionRefreshInterval — как часто перечитывается `xkeen -v`.
	xkeenVersionRefreshInterval = 10 * time.Minute
	// xkeenVersionRetryInterval — повтор, пока версию ни разу не удалось получить.
	xkeenVersionRetryInterval = time.Minute
	// xkeenStatusStaleAfter — снимок старше этого возраста считается устаревшим.
	xkeenStatusStaleAfter = 2 * XKeenStatusPollInterval
	// xkeenStatusErrMax — предел длины текста последней ошибки опроса.
	xkeenStatusErrMax = 200
)

// XKeenStatusSnapshot — последнее известное состояние XKeen.
type XKeenStatusSnapshot struct {
	// Raw — вывод последнего УСПЕШНОГО `xkeen -status`.
	Raw string
	// Version — версия из последнего успешного `xkeen -v`; пусто, пока не получена.
	Version string
	// UpdatedAt — момент последнего успешного опроса статуса; нулевой на холодном старте.
	UpdatedAt time.Time
	// VersionUpdatedAt — момент последнего успешного чтения версии.
	VersionUpdatedAt time.Time
	// Stale — снимок нельзя выдавать за свежий: последний опрос неуспешен, кэш
	// инвалидирован и ещё не обновлён, возраст больше двух интервалов, либо
	// опросов ещё не было.
	Stale bool
	// LastErr — текст ошибки последнего опроса (только для логов и тестов,
	// в HTTP-ответы не попадает).
	LastErr string
}

// AgeSeconds — возраст последнего успешного опроса в секундах;
// ok=false, если успешных опросов не было.
func (s XKeenStatusSnapshot) AgeSeconds(now time.Time) (int, bool) {
	if s.UpdatedAt.IsZero() {
		return 0, false
	}
	age := now.Sub(s.UpdatedAt)
	if age < 0 {
		age = 0
	}
	return int(age / time.Second), true
}

// XKeenStatusCache единолично опрашивает `xkeen -status` и `xkeen -v` в фоне.
// Обработчики HTTP читают снимок и не запускают процесс xkeen на пути запроса.
type XKeenStatusCache struct {
	runStatus  func(ctx context.Context) (string, error)
	runVersion func(ctx context.Context) string
	now        func() time.Time
	interval   time.Duration

	mu   sync.Mutex
	snap XKeenStatusSnapshot
	// invalGen растёт при каждой инвалидации; cleanGen — поколение, которое уже
	// покрыто успешным опросом. Инвалидация во время опроса не теряется.
	invalGen        uint64
	cleanGen        uint64
	versionInvalGen uint64
	versionCleanGen uint64
	versionTriedAt  time.Time
	waiters         []chan struct{}

	refreshCh chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	wg        sync.WaitGroup
}

// NewXKeenStatusCache создаёт кэш поверх сервиса XKeen. Кэш создаётся только в
// cmd/xcp/main.go; Start запускает опрос, Stop (через defer) останавливает.
func NewXKeenStatusCache(x *XKeenService) *XKeenStatusCache {
	return newXKeenStatusCacheFunc(
		func(context.Context) (string, error) {
			if !x.Installed() {
				return "", ErrXKeenNotInstalled
			}
			return x.StatusWithTimeout(xkeenStatusPollTimeout)
		},
		func(context.Context) string {
			if !x.Installed() {
				return ""
			}
			return x.GetVersion()
		},
		time.Now,
		XKeenStatusPollInterval,
	)
}

// NewXKeenStatusCacheFunc собирает кэш из готовых функций опроса: для тестов
// других пакетов, где настоящий xkeen не запускается.
func NewXKeenStatusCacheFunc(
	runStatus func(ctx context.Context) (string, error),
	runVersion func(ctx context.Context) string,
	now func() time.Time,
	interval time.Duration,
) *XKeenStatusCache {
	return newXKeenStatusCacheFunc(runStatus, runVersion, now, interval)
}

func newXKeenStatusCacheFunc(
	runStatus func(ctx context.Context) (string, error),
	runVersion func(ctx context.Context) string,
	now func() time.Time,
	interval time.Duration,
) *XKeenStatusCache {
	if now == nil {
		now = time.Now
	}
	if runVersion == nil {
		runVersion = func(context.Context) string { return "" }
	}
	return &XKeenStatusCache{
		runStatus:  runStatus,
		runVersion: runVersion,
		now:        now,
		interval:   interval,
		refreshCh:  make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
	}
}

// Start запускает фоновый цикл; повторный вызов ничего не делает.
func (c *XKeenStatusCache) Start() {
	c.startOnce.Do(func() {
		c.wg.Add(1)
		go c.loop()
	})
}

// Stop останавливает цикл и ждёт его завершения. Безопасен при повторном
// вызове и без Start.
func (c *XKeenStatusCache) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	c.wg.Wait()
}

func (c *XKeenStatusCache) loop() {
	defer c.wg.Done()

	// Первый опрос сразу, дальше пауза отсчитывается от завершения опроса.
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-timer.C:
		case <-c.refreshCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		select {
		case <-c.stopCh:
			return
		default:
		}
		c.pollOnce()
		timer.Reset(c.interval)
	}
}

// pollOnce блокируется до конца процесса xkeen: параллельных запусков нет,
// потому что вызывается только из loop.
func (c *XKeenStatusCache) pollOnce() {
	c.mu.Lock()
	gen := c.invalGen
	vGen := c.versionInvalGen
	waiters := c.waiters
	c.waiters = nil
	// Просьба об опросе, пришедшая до этого момента, покрыта текущим опросом
	// (gen и сигнал меняются под одним замком), лишний запуск не нужен.
	select {
	case <-c.refreshCh:
	default:
	}
	c.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-c.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	raw, err := c.runStatus(ctx)

	c.mu.Lock()
	now := c.now()
	if err == nil {
		c.snap.Raw = raw
		c.snap.UpdatedAt = now
		c.snap.LastErr = ""
		c.cleanGen = gen
	} else {
		c.snap.LastErr = sanitizeStatusErr(err.Error())
	}
	needVersion := c.versionDueLocked(now)
	if c.invalGen != gen {
		// инвалидация пришла во время опроса — следующий сразу
		c.signalLocked()
	}
	c.mu.Unlock()

	if needVersion {
		v := strings.TrimSpace(c.runVersion(ctx))
		c.mu.Lock()
		now = c.now()
		c.versionTriedAt = now
		if v != "" && v != "unknown" {
			c.snap.Version = v
			c.snap.VersionUpdatedAt = now
			c.versionCleanGen = vGen
		}
		c.mu.Unlock()
	}

	for _, w := range waiters {
		close(w)
	}
}

// versionDueLocked — пора ли перечитать `xkeen -v`. Вызывать под c.mu.
func (c *XKeenStatusCache) versionDueLocked(now time.Time) bool {
	if c.versionInvalGen != c.versionCleanGen {
		return true
	}
	if c.versionTriedAt.IsZero() {
		return true
	}
	wait := xkeenVersionRefreshInterval
	if c.snap.Version == "" {
		wait = xkeenVersionRetryInterval
	}
	return now.Sub(c.versionTriedAt) >= wait
}

// Snapshot возвращает копию последнего состояния.
func (c *XKeenStatusCache) Snapshot() XKeenStatusSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snap
	switch {
	case s.UpdatedAt.IsZero():
		s.Stale = true
	case s.LastErr != "":
		s.Stale = true
	case c.invalGen != c.cleanGen:
		s.Stale = true
	case c.now().Sub(s.UpdatedAt) > xkeenStatusStaleAfter:
		s.Stale = true
	}
	return s
}

// AgeSeconds — возраст снимка по часам кэша (ok=false на холодном старте).
func (c *XKeenStatusCache) AgeSeconds(s XKeenStatusSnapshot) (int, bool) {
	c.mu.Lock()
	now := c.now()
	c.mu.Unlock()
	return s.AgeSeconds(now)
}

// Invalidate помечает снимок устаревшим и просит внеочередной опрос.
func (c *XKeenStatusCache) Invalidate() {
	c.mu.Lock()
	c.invalGen++
	c.signalLocked()
	c.mu.Unlock()
}

// signalLocked неблокирующе будит цикл. Вызывать под c.mu.
func (c *XKeenStatusCache) signalLocked() {
	select {
	case c.refreshCh <- struct{}{}:
	default:
	}
}

// InvalidateVersion дополнительно требует перечитать версию (после установки XKeen).
func (c *XKeenStatusCache) InvalidateVersion() {
	c.mu.Lock()
	c.versionInvalGen++
	c.invalGen++
	c.signalLocked()
	c.mu.Unlock()
}

// RefreshNow запрашивает внеочередной опрос и ждёт его завершения (или ctx).
func (c *XKeenStatusCache) RefreshNow(ctx context.Context) XKeenStatusSnapshot {
	w := make(chan struct{})
	c.mu.Lock()
	c.waiters = append(c.waiters, w)
	c.invalGen++
	c.signalLocked()
	c.mu.Unlock()
	select {
	case <-w:
	case <-ctx.Done():
	case <-c.stopCh:
	}
	return c.Snapshot()
}

// sanitizeStatusErr убирает ANSI и режет текст до xkeenStatusErrMax байт по границе руны.
func sanitizeStatusErr(msg string) string {
	msg = strings.TrimSpace(utils.StripANSI(msg))
	if len(msg) <= xkeenStatusErrMax {
		return msg
	}
	msg = msg[:xkeenStatusErrMax]
	for !utf8.ValidString(msg) {
		msg = msg[:len(msg)-1]
	}
	return msg
}
