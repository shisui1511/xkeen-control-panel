package services_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi/testutil"
)

func TestXrayGRPC_Lifecycle(t *testing.T) {
	mock := testutil.NewMockStatsServer()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	go func() {
		_ = s.Serve(lis)
	}()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("passthrough://bufnet")
	svc.SetIdleDuration(50 * time.Millisecond)
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(target,
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})

	// 1. Connection is nil before first Acquire
	if svc.IsConnected() {
		t.Errorf("expected connection to be nil initially")
	}

	// 2. Acquire connects
	ctx := context.Background()
	client, release, err := svc.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if client == nil {
		t.Fatalf("expected client, got nil")
	}
	if !svc.IsConnected() {
		t.Errorf("expected connection to be open after Acquire")
	}

	// 3. Release and check idle cleanup
	release()
	if !svc.IsConnected() {
		t.Errorf("expected connection to stay open during idle window")
	}

	// Wait for idle timer to close connection
	time.Sleep(100 * time.Millisecond)
	if svc.IsConnected() {
		t.Errorf("expected connection to close after idle timeout")
	}

	// 4. Re-acquire works
	client2, release2, err := svc.Acquire(ctx)
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	if client2 == nil {
		t.Fatalf("expected client2, got nil")
	}
	if !svc.IsConnected() {
		t.Errorf("expected connection to reopen on second Acquire")
	}

	// 5. Stop closes connection and prevents further Acquire
	svc.Stop()
	release2()

	if svc.IsConnected() {
		t.Errorf("expected connection to be closed after Stop")
	}

	_, _, err = svc.Acquire(ctx)
	if err == nil {
		t.Errorf("expected error from Acquire after Stop")
	}
}

func TestXrayGRPC_Concurrency(t *testing.T) {
	mock := testutil.NewMockStatsServer()
	mock.SetStat("outbound>>>test>>>traffic>>>uplink", 100)
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	go func() {
		_ = s.Serve(lis)
	}()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("passthrough://bufnet")
	svc.SetIdleDuration(100 * time.Millisecond)
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(target,
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})

	var wg sync.WaitGroup
	workers := 10
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			client, release, err := svc.Acquire(ctx)
			if err != nil {
				t.Errorf("concurrent Acquire failed: %v", err)
				return
			}
			defer release()

			traffic, err := client.OutboundTraffic(ctx)
			if err != nil {
				t.Errorf("OutboundTraffic failed: %v", err)
				return
			}
			if traffic["test"].Uplink != 100 {
				t.Errorf("unexpected traffic value: %+v", traffic["test"])
			}
		}()
	}

	wg.Wait()
	svc.Stop()
}

func TestXrayGRPC_KernelCheck(t *testing.T) {
	svc := services.NewXrayGRPCService("127.0.0.1:10085")
	activeKernel := "mihomo"
	svc.SetActiveKernelFunc(func() string {
		return activeKernel
	})

	// When kernel is mihomo, Acquire should fail and NOT connect
	ctx := context.Background()
	_, _, err := svc.Acquire(ctx)
	if err == nil {
		t.Fatalf("expected error when kernel is mihomo, got nil")
	}
	if svc.IsConnected() {
		t.Errorf("connection was opened even though active kernel is not Xray")
	}

	// Switch kernel to xray
	activeKernel = "xray"
	// Now it won't fail the kernel check (it will dial or fail dial)
	// We don't have server running, but it passes kernel check
}
