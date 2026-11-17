// Package proxyping выполняет пачечный пинг узлов страницы «Прокси» (фаза 150).
// Туннель проверяется временным процессом Xray, как выбрано в пробе 143 Q8.
package proxyping

import (
	"errors"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

const (
	DefaultTmpRoot       = "/opt/tmp/xcp-ping"
	ProcName             = "xcp-pingcore"
	MethodXray           = "xray"
	MethodMihomo         = "mihomo"
	MethodTLS            = "tls"
	ScopeAll             = "all"
	ScopeGroup           = "group"
	ScopeRepick          = "repick" // пачка Probe: группа есть всегда (pickAlive задаёт Group)
	StatusOK             = "ok"
	StatusTimeout        = "timeout"
	StatusError          = "error"
	StateRunning         = "running"
	StateDone            = "done"
	StateCancelled       = "cancelled"
	StateKernelChanged   = "kernel_changed"
	StateFailed          = "failed"
	FallbackNoXrayBinary = "no_xray_binary"
	FallbackLowMemory    = "low_memory"
	// FallbackXrayRejected — временный Xray куска вышел, не дойдя до готовности
	// (отверг исходящее одного из узлов): узлы куска проверены по TLS.
	FallbackXrayRejected = "xray_rejected"
	ReasonTimeout        = "timeout"
	ReasonConnect        = "connect_failed"
	ReasonTunnel         = "tunnel_failed"
	ReasonTLS            = "tls_failed"
	ReasonResolve        = "resolve_failed"
	ReasonDelay          = "delay_failed"
	ReasonStart          = "start_failed"
)

var (
	ErrNoTargets = errors.New("ping_no_targets")
	ErrStopped   = errors.New("ping_stopped")
	// ErrBusy — идёт пачка другого охвата или другой группы: пользовательский
	// запуск не присоединяется к ней молча (ping_busy).
	ErrBusy = errors.New("ping_busy")
)

type Target struct {
	Ref        string
	Node       nodes.Node
	MihomoName string
	// MihomoProvider — провайдер профиля, в файле которого лежит узел под
	// MihomoName: имя узла уникально только внутри провайдера, и замер идёт через
	// него. Пусто — MihomoName ищется в /proxies (копия узла в proxies профиля,
	// имя уникально).
	MihomoProvider string
	Xray           bool
	Fingerprint    string
}

type Request struct {
	Scope, Group, Kernel, Method string
	Targets                      []Target
	URL                          string
	Timeout                      time.Duration
}

type PingBatch struct {
	ID             string     `json:"id"`
	State          string     `json:"state"`
	Scope          string     `json:"scope"`
	Group          string     `json:"group,omitempty"`
	Kernel         string     `json:"kernel"`
	Method         string     `json:"method"`
	Total          int        `json:"total"`
	Done           int        `json:"done"`
	Excluded       int        `json:"excluded"`
	FallbackReason string     `json:"fallback_reason,omitempty"`
	ReasonCode     string     `json:"reason_code,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type PingPoint struct {
	MS     int       `json:"ms"`
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

type PingResult struct {
	MS      int         `json:"ms"`
	Status  string      `json:"status"`
	Reason  string      `json:"reason,omitempty"`
	At      time.Time   `json:"at"`
	Kernel  string      `json:"kernel"`
	Method  string      `json:"method"`
	History []PingPoint `json:"history"`
}

type PingSnapshot struct {
	Batch   *PingBatch            `json:"batch,omitempty"`
	Results map[string]PingResult `json:"results"`
}

type PingStart struct {
	BatchID string    `json:"batch_id"`
	Joined  bool      `json:"joined"`
	Batch   PingBatch `json:"batch"`
}
