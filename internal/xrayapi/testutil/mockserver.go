package testutil

import (
	"context"
	"net"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
)

const bufSize = 1024 * 1024

// MockStatsServer implements statspb.StatsServiceServer for tests.
type MockStatsServer struct {
	statspb.UnimplementedStatsServiceServer
	mu       sync.RWMutex
	stats    map[string]int64
	sysStats *statspb.SysStatsResponse
}

// NewMockStatsServer creates a new in-memory MockStatsServer.
func NewMockStatsServer() *MockStatsServer {
	return &MockStatsServer{
		stats: make(map[string]int64),
	}
}

// SetStat sets a counter value for a given metric name.
func (s *MockStatsServer) SetStat(name string, val int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats[name] = val
}

// SetSysStats sets the system statistics response.
func (s *MockStatsServer) SetSysStats(sys *statspb.SysStatsResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sysStats = sys
}

// GetStats returns a single stat counter.
func (s *MockStatsServer) GetStats(ctx context.Context, req *statspb.GetStatsRequest) (*statspb.GetStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val := s.stats[req.GetName()]
	return &statspb.GetStatsResponse{
		Stat: &statspb.Stat{
			Name:  req.GetName(),
			Value: val,
		},
	}, nil
}

// QueryStats queries matching stats by pattern.
func (s *MockStatsServer) QueryStats(ctx context.Context, req *statspb.QueryStatsRequest) (*statspb.QueryStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*statspb.Stat
	for k, v := range s.stats {
		if strings.Contains(k, req.GetPattern()) {
			list = append(list, &statspb.Stat{Name: k, Value: v})
		}
	}
	return &statspb.QueryStatsResponse{Stat: list}, nil
}

// GetSysStats returns system stats.
func (s *MockStatsServer) GetSysStats(ctx context.Context, req *statspb.SysStatsRequest) (*statspb.SysStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sysStats != nil {
		return s.sysStats, nil
	}
	return &statspb.SysStatsResponse{
		NumGoroutine: 10,
		Alloc:        1024 * 1024,
	}, nil
}

// StartMockStatsServer starts an in-process bufconn gRPC server with the given MockStatsServer.
// It returns a connected *grpc.ClientConn and a cleanup function.
func StartMockStatsServer(mock *MockStatsServer) (*grpc.ClientConn, func(), error) {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)

	go func() {
		_ = s.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		s.Stop()
		_ = lis.Close()
		return nil, nil, err
	}

	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}

	return conn, cleanup, nil
}
