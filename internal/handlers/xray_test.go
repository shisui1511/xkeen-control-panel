package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi/testutil"
)

func TestXrayRealityKeygen(t *testing.T) {
	api := &API{}

	req := httptest.NewRequest(http.MethodGet, "/api/xray/reality/keygen", nil)
	rr := httptest.NewRecorder()

	api.XrayRealityKeygen(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			PrivateKey string `json:"private_key"`
			PublicKey  string `json:"public_key"`
			ShortID    string `json:"short_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success true")
	}
	if resp.Data.PrivateKey == "" || resp.Data.PublicKey == "" || resp.Data.ShortID == "" {
		t.Errorf("expected non-empty keypair in response: %+v", resp.Data)
	}
}

func TestXrayStats(t *testing.T) {
	// 1. Nil service -> 503
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, "/api/xray/stats", nil)
	rr := httptest.NewRecorder()
	api.XrayStats(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when service is nil, got %d", rr.Code)
	}

	// 2. Non-GET method -> 405
	reqPost := httptest.NewRequest(http.MethodPost, "/api/xray/stats", nil)
	rrPost := httptest.NewRecorder()
	api.XrayStats(rrPost, reqPost)
	if rrPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST, got %d", rrPost.Code)
	}

	// 3. Kernel not xray -> 503
	svcKernel := services.NewXrayGRPCService("127.0.0.1:10085")
	svcKernel.SetActiveKernelFunc(func() string { return "mihomo" })
	api.SetXrayGRPCService(svcKernel)
	rrKernel := httptest.NewRecorder()
	api.XrayStats(rrKernel, req)
	if rrKernel.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when kernel is mihomo, got %d", rrKernel.Code)
	}

	// 4. Success path with mock server
	mock := testutil.NewMockStatsServer()
	mock.SetStat("outbound>>>vless-us>>>traffic>>>uplink", 1024)
	mock.SetStat("outbound>>>vless-us>>>traffic>>>downlink", 2048)

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	go func() { _ = s.Serve(lis) }()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("passthrough://bufnet")
	svc.SetActiveKernelFunc(func() string { return "xray" })
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(target,
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})
	api.SetXrayGRPCService(svc)

	rrSuccess := httptest.NewRecorder()
	api.XrayStats(rrSuccess, req)

	if rrSuccess.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrSuccess.Code, rrSuccess.Body.String())
	}

	var statsResp struct {
		Success bool `json:"success"`
		Data    map[string]struct {
			Uplink   int64 `json:"uplink"`
			Downlink int64 `json:"downlink"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rrSuccess.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}
	if !statsResp.Success {
		t.Errorf("expected success true")
	}
	vless, ok := statsResp.Data["vless-us"]
	if !ok {
		t.Fatalf("expected vless-us in stats")
	}
	if vless.Uplink != 1024 || vless.Downlink != 2048 {
		t.Errorf("expected 1024/2048, got %d/%d", vless.Uplink, vless.Downlink)
	}
}
