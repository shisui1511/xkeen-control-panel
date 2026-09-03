package xrayapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi/testutil"
)

func TestOutboundTraffic(t *testing.T) {
	mock := testutil.NewMockStatsServer()
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>uplink", 1024)
	mock.SetStat("outbound>>>proxy-1>>>traffic>>>downlink", 4096)
	mock.SetStat("outbound>>>direct>>>traffic>>>uplink", 512)
	mock.SetStat("outbound>>>direct>>>traffic>>>downlink", 2048)
	mock.SetStat("inbound>>>api>>>traffic>>>uplink", 9999) // Should be ignored

	conn, cleanup, err := testutil.StartMockStatsServer(mock)
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

func TestSysStats(t *testing.T) {
	mock := testutil.NewMockStatsServer()
	mock.SetSysStats(&statspb.SysStatsResponse{
		NumGoroutine: 42,
		Alloc:        8192,
	})

	conn, cleanup, err := testutil.StartMockStatsServer(mock)
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
