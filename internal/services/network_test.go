package services

import (
	"testing"
)

func TestNetworkToolsService_New(t *testing.T) {
	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	svc.SetMihomoService(nil)
}

func TestNetworkToolsService_GetPublicIP(t *testing.T) {
	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.GetPublicIP()
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}
	// In test/CI environment without internet or echo servers, result may succeed or return error message gracefully
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}
