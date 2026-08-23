package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
