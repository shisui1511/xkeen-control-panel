package testutil

import (
	"context"
	"net"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	logpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/log/command"
	routerpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/router/command"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
)

const bufSize = 1024 * 1024

// MockXrayServer implements stats, router, and logger services for testing.
type MockXrayServer struct {
	statspb.UnimplementedStatsServiceServer
	routerpb.UnimplementedRoutingServiceServer
	logpb.UnimplementedLoggerServiceServer

	mu sync.RWMutex

	// Stats
	stats    map[string]int64
	sysStats *statspb.SysStatsResponse
	statsErr error

	// Router
	routeResults        map[string]*routerpb.RoutingContext
	defaultRouteOutbound string
	defaultRouteGroups   []string
	routeErr            error
	LastRoutingContext  *routerpb.RoutingContext
	LastPublishResult   bool

	// Logger
	restartLoggerCount int
	loggerErr          error
}

// MockStatsServer is an alias to MockXrayServer for backwards compatibility.
type MockStatsServer = MockXrayServer

// NewMockXrayServer creates a new in-memory MockXrayServer.
func NewMockXrayServer() *MockXrayServer {
	return &MockXrayServer{
		stats:        make(map[string]int64),
		routeResults: make(map[string]*routerpb.RoutingContext),
	}
}

// NewMockStatsServer creates a new MockStatsServer.
func NewMockStatsServer() *MockStatsServer {
	return NewMockXrayServer()
}

// SetStat sets a counter value for a given metric name.
func (s *MockXrayServer) SetStat(name string, val int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats[name] = val
}

// SetSysStats sets the system statistics response.
func (s *MockXrayServer) SetSysStats(sys *statspb.SysStatsResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sysStats = sys
}

// SetStatsError sets an error to be returned by StatsService calls.
func (s *MockXrayServer) SetStatsError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsErr = err
}

// SetRouteResult sets a route result for a specific domain or target.
func (s *MockXrayServer) SetRouteResult(target string, outboundTag string, groupTags []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routeResults[target] = &routerpb.RoutingContext{
		OutboundTag:       outboundTag,
		OutboundGroupTags: groupTags,
	}
}

// SetDefaultRoute sets the fallback route result when target is not specifically matched.
func (s *MockXrayServer) SetDefaultRoute(outboundTag string, groupTags []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultRouteOutbound = outboundTag
	s.defaultRouteGroups = groupTags
}

// SetRouteError sets an error to be returned by RoutingService calls.
func (s *MockXrayServer) SetRouteError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routeErr = err
}

// SetLoggerError sets an error to be returned by LoggerService calls.
func (s *MockXrayServer) SetLoggerError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loggerErr = err
}

// RestartLoggerCount returns the number of times RestartLogger was called.
func (s *MockXrayServer) RestartLoggerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restartLoggerCount
}

// GetStats returns a single stat counter.
func (s *MockXrayServer) GetStats(ctx context.Context, req *statspb.GetStatsRequest) (*statspb.GetStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	val := s.stats[req.GetName()]
	return &statspb.GetStatsResponse{
		Stat: &statspb.Stat{
			Name:  req.GetName(),
			Value: val,
		},
	}, nil
}

// QueryStats queries matching stats by pattern.
func (s *MockXrayServer) QueryStats(ctx context.Context, req *statspb.QueryStatsRequest) (*statspb.QueryStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	var list []*statspb.Stat
	for k, v := range s.stats {
		if strings.Contains(k, req.GetPattern()) {
			list = append(list, &statspb.Stat{Name: k, Value: v})
		}
	}
	return &statspb.QueryStatsResponse{Stat: list}, nil
}

// GetSysStats returns system stats.
func (s *MockXrayServer) GetSysStats(ctx context.Context, req *statspb.SysStatsRequest) (*statspb.SysStatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	if s.sysStats != nil {
		return s.sysStats, nil
	}
	return &statspb.SysStatsResponse{
		NumGoroutine: 10,
		Alloc:        1024 * 1024,
	}, nil
}

// TestRoute implements routerpb.RoutingServiceServer.
func (s *MockXrayServer) TestRoute(ctx context.Context, req *routerpb.TestRouteRequest) (*routerpb.RoutingContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routeErr != nil {
		return nil, s.routeErr
	}

	rc := req.GetRoutingContext()
	s.LastRoutingContext = rc
	s.LastPublishResult = req.GetPublishResult()

	if rc != nil {
		if res, ok := s.routeResults[rc.GetTargetDomain()]; ok {
			return res, nil
		}
	}

	if s.defaultRouteOutbound != "" {
		return &routerpb.RoutingContext{
			OutboundTag:       s.defaultRouteOutbound,
			OutboundGroupTags: s.defaultRouteGroups,
		}, nil
	}

	// No match
	return &routerpb.RoutingContext{
		OutboundTag: "",
	}, nil
}

// RestartLogger implements logpb.LoggerServiceServer.
func (s *MockXrayServer) RestartLogger(ctx context.Context, req *logpb.RestartLoggerRequest) (*logpb.RestartLoggerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loggerErr != nil {
		return nil, s.loggerErr
	}
	s.restartLoggerCount++
	return &logpb.RestartLoggerResponse{}, nil
}

// StartMockXrayServer starts an in-process bufconn gRPC server registering Stats, Router, and Logger.
func StartMockXrayServer(mock *MockXrayServer) (*grpc.ClientConn, func(), error) {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	routerpb.RegisterRoutingServiceServer(s, mock)
	logpb.RegisterLoggerServiceServer(s, mock)

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

// StartMockStatsServer starts an in-process bufconn gRPC server with the given MockStatsServer.
func StartMockStatsServer(mock *MockStatsServer) (*grpc.ClientConn, func(), error) {
	return StartMockXrayServer(mock)
}
