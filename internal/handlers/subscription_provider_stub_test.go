package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

const (
	stubShareLink = "vless://00000000-0000-0000-0000-000000000000@0.0.0.0:1?security=none#stub"
	okShareLink   = "vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=none#ok"
)

func providerAdapterGet(t *testing.T, api *API, upstreamURL string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+url.QueryEscape(upstreamURL), nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	return rr
}

func TestMihomoProviderAdapter_DropsStubNodes(t *testing.T) {
	api, subSvc := newSubTestAPI(t)
	api.cfg.MihomoConfigDir = api.cfg.DataDir

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(stubShareLink + "\n" + okShareLink + "\n"))
	}))
	defer ts.Close()

	sub := &services.Subscription{Name: "Stub Sub", URL: ts.URL, Enabled: true, EnableMihomo: true}
	if err := subSvc.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	rr := providerAdapterGet(t, api, ts.URL)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "1.2.3.4") {
		t.Errorf("working node must stay in payload, got: %s", body)
	}
	if strings.Contains(body, "0.0.0.0") {
		t.Errorf("stub node must be dropped from payload, got: %s", body)
	}
}
