package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi"
)

var (
	// ErrKernelNotXray is returned when Acquire is called while Xray is not the active kernel.
	ErrKernelNotXray = errors.New("Xray is not the active kernel")
	// ErrServiceStopped is returned when Acquire is called after Stop().
	ErrServiceStopped = errors.New("Xray gRPC service stopped")
)

// XrayGRPCService manages an on-demand, reference-counted gRPC connection to Xray.
type XrayGRPCService struct {
	addr           string
	mu             sync.Mutex
	conn           *grpc.ClientConn
	refCount       int
	idleTimer      *time.Timer
	idleDuration   time.Duration
	stopped        bool
	activeKernelFn func() string
	dialerFn       func(ctx context.Context, target string) (*grpc.ClientConn, error)
}

// NewXrayGRPCService creates a new XrayGRPCService for the given target address.
func NewXrayGRPCService(addr string) *XrayGRPCService {
	return &XrayGRPCService{
		addr:         addr,
		idleDuration: 30 * time.Second,
	}
}

// SetIdleDuration sets the duration after which an idle connection is closed.
func (s *XrayGRPCService) SetIdleDuration(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idleDuration = d
}

// SetActiveKernelFunc sets the callback that determines the currently active kernel.
func (s *XrayGRPCService) SetActiveKernelFunc(fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeKernelFn = fn
}

// SetDialerFunc allows injecting a custom dialer (e.g. for testing with bufconn).
func (s *XrayGRPCService) SetDialerFunc(fn func(ctx context.Context, target string) (*grpc.ClientConn, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialerFn = fn
}

// Start starts the service.
func (s *XrayGRPCService) Start() {
	// Ready for on-demand connections
}

// Acquire gets or creates an on-demand gRPC connection and returns an xrayapi.Client
// along with a release function. The caller MUST call release() when finished.
func (s *XrayGRPCService) Acquire(ctx context.Context) (*xrayapi.Client, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return nil, nil, ErrServiceStopped
	}

	if s.activeKernelFn != nil {
		kernel := s.activeKernelFn()
		if kernel != "xray" {
			return nil, nil, fmt.Errorf("%w (active: %s)", ErrKernelNotXray, kernel)
		}
	}

	if s.conn == nil {
		var conn *grpc.ClientConn
		var err error
		if s.dialerFn != nil {
			conn, err = s.dialerFn(ctx, s.addr)
		} else {
			conn, err = grpc.NewClient(
				s.addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to connect to Xray gRPC: %w", err)
		}
		s.conn = conn
	}

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	s.refCount++

	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.refCount--
			if s.refCount <= 0 {
				s.refCount = 0
				if !s.stopped && s.conn != nil {
					if s.idleTimer != nil {
						s.idleTimer.Stop()
					}
					s.idleTimer = time.AfterFunc(s.idleDuration, func() {
						s.mu.Lock()
						defer s.mu.Unlock()
						if s.refCount == 0 && s.conn != nil && !s.stopped {
							_ = s.conn.Close()
							s.conn = nil
						}
					})
				}
			}
		})
	}

	return xrayapi.NewClient(s.conn), release, nil
}

// Stop closes the connection and marks the service stopped.
func (s *XrayGRPCService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// IsConnected returns true if the gRPC connection is currently open.
func (s *XrayGRPCService) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}
