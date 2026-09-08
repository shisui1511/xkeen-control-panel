package xrayapi_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	netpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/common/net"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi/testutil"
)

func TestOutboundTraffic(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>uplink", 1024)
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>downlink", 4096)
	mock.SetStat("outbound>>>direct>>>traffic>>>uplink", 512)
	mock.SetStat("outbound>>>direct>>>traffic>>>downlink", 2048)
	mock.SetStat("inbound>>>api>>>traffic>>>uplink", 9999) // Should be ignored

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	traffic, err := client.OutboundTraffic(ctx)
	if err != nil {
		t.Fatalf("OutboundTraffic failed: %v", err)
	}

	if len(traffic) != 2 {
		t.Fatalf("expected 2 outbound items, got %d", len(traffic))
	}

	p1, ok := traffic["proxy-1"]
	if !ok {
		t.Fatalf("missing proxy-1 in traffic")
	}
	if p1.Uplink != 1024 || p1.Downlink != 4096 {
		t.Errorf("proxy-1 traffic mismatch: got %+v", p1)
	}

	dir, ok := traffic["direct"]
	if !ok {
		t.Fatalf("missing direct in traffic")
	}
	if dir.Uplink != 512 || dir.Downlink != 2048 {
		t.Errorf("direct traffic mismatch: got %+v", dir)
	}
}

func TestQueryAllTraffic(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>uplink", 1024)
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>downlink", 4096)
	mock.SetStat("inbound>>>tproxy>>>traffic>>>uplink", 2048)
	mock.SetStat("inbound>>>tproxy>>>traffic>>>downlink", 8192)
	mock.SetStat("user>>>alice@example.com>>>traffic>>>uplink", 512)
	mock.SetStat("user>>>alice@example.com>>>traffic>>>downlink", 1024)
	mock.SetStat("other>>>something>>>traffic>>>uplink", 9999) // Unknown scope, should be ignored

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stats, err := client.QueryAllTraffic(ctx)
	if err != nil {
		t.Fatalf("QueryAllTraffic failed: %v", err)
	}

	if len(stats.Outbounds) != 1 || stats.Outbounds["proxy-1"].Uplink != 1024 || stats.Outbounds["proxy-1"].Downlink != 4096 {
		t.Errorf("unexpected outbounds: %+v", stats.Outbounds)
	}
	if len(stats.Inbounds) != 1 || stats.Inbounds["tproxy"].Uplink != 2048 || stats.Inbounds["tproxy"].Downlink != 8192 {
		t.Errorf("unexpected inbounds: %+v", stats.Inbounds)
	}
	if len(stats.Users) != 1 || stats.Users["alice@example.com"].Uplink != 512 || stats.Users["alice@example.com"].Downlink != 1024 {
		t.Errorf("unexpected users: %+v", stats.Users)
	}
}

func TestSysStats(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetSysStats(&statspb.SysStatsResponse{
		NumGoroutine: 42,
		Alloc:        8192,
	})

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sys, err := client.SysStats(ctx)
	if err != nil {
		t.Fatalf("SysStats failed: %v", err)
	}
	if sys.GetNumGoroutine() != 42 {
		t.Errorf("expected NumGoroutine=42, got %d", sys.GetNumGoroutine())
	}
	if sys.GetAlloc() != 8192 {
		t.Errorf("expected Alloc=8192, got %d", sys.GetAlloc())
	}
}

func TestTestRoute_Domain(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetRouteResult("example.com", "proxy-out", []string{"group-auto", "group-all"})

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	res, err := client.TestRoute(ctx, xrayapi.RouteTestInput{
		Domain:     "example.com",
		Port:       443,
		Network:    "tcp",
		InboundTag: "in-direct",
	})
	if err != nil {
		t.Fatalf("TestRoute failed: %v", err)
	}

	if !res.Matched {
		t.Errorf("expected matched=true")
	}
	if res.OutboundTag != "proxy-out" {
		t.Errorf("expected outbound tag proxy-out, got %s", res.OutboundTag)
	}
	if len(res.OutboundGroupTags) != 2 || res.OutboundGroupTags[0] != "group-auto" {
		t.Errorf("unexpected outbound group tags: %v", res.OutboundGroupTags)
	}

	// Verify sent RoutingContext & PublishResult=false
	if mock.LastPublishResult {
		t.Errorf("expected PublishResult=false, got true")
	}
	if mock.LastRoutingContext == nil {
		t.Fatalf("expected LastRoutingContext to be recorded")
	}
	if mock.LastRoutingContext.GetTargetDomain() != "example.com" {
		t.Errorf("expected domain example.com, got %s", mock.LastRoutingContext.GetTargetDomain())
	}
	if mock.LastRoutingContext.GetNetwork() != netpb.Network_TCP {
		t.Errorf("expected Network_TCP, got %v", mock.LastRoutingContext.GetNetwork())
	}
}

func TestTestRoute_IP(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetDefaultRoute("direct", nil)

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	res, err := client.TestRoute(ctx, xrayapi.RouteTestInput{
		IP:   "1.1.1.1",
		Port: 53,
	})
	if err != nil {
		t.Fatalf("TestRoute failed: %v", err)
	}
	if res.OutboundTag != "direct" {
		t.Errorf("expected direct, got %s", res.OutboundTag)
	}

	if mock.LastRoutingContext == nil || len(mock.LastRoutingContext.GetTargetIPs()) == 0 {
		t.Fatalf("expected TargetIPs to be set")
	}
	expectedIP := net.ParseIP("1.1.1.1").To4()
	if string(mock.LastRoutingContext.GetTargetIPs()[0]) != string(expectedIP) {
		t.Errorf("expected IP %v, got %v", expectedIP, mock.LastRoutingContext.GetTargetIPs()[0])
	}
}

func TestTestRoute_NoMatch(t *testing.T) {
	mock := testutil.NewMockXrayServer()

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	res, err := client.TestRoute(ctx, xrayapi.RouteTestInput{
		Domain: "unmatched-domain.org",
	})
	if err != nil {
		t.Fatalf("TestRoute failed on no match: %v", err)
	}

	if res.Matched {
		t.Errorf("expected Matched=false for empty response")
	}
	if res.OutboundTag != "" {
		t.Errorf("expected empty outbound tag, got %s", res.OutboundTag)
	}
}

func TestTestRoute_Unavailable(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetRouteError(status.Error(codes.Unavailable, "connection refused"))

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	_, err = client.TestRoute(ctx, xrayapi.RouteTestInput{
		Domain: "example.com",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, xrayapi.ErrCoreUnavailable) {
		t.Errorf("expected ErrCoreUnavailable, got %v", err)
	}
}

func TestRestartLogger(t *testing.T) {
	mock := testutil.NewMockXrayServer()

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	err = client.RestartLogger(ctx)
	if err != nil {
		t.Fatalf("RestartLogger failed: %v", err)
	}
	if mock.RestartLoggerCount() != 1 {
		t.Errorf("expected RestartLoggerCount=1, got %d", mock.RestartLoggerCount())
	}
}

func TestRestartLogger_Unavailable(t *testing.T) {
	mock := testutil.NewMockXrayServer()
	mock.SetLoggerError(status.Error(codes.Unavailable, "core down"))

	conn, cleanup, err := testutil.StartMockXrayServer(mock)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer cleanup()

	client := xrayapi.NewClient(conn)
	ctx := context.Background()

	err = client.RestartLogger(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, xrayapi.ErrCoreUnavailable) {
		t.Errorf("expected ErrCoreUnavailable, got %v", err)
	}
}
