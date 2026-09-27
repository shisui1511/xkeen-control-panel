package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

func TestMihomoProviderAdapter_AllStubsServesCachedPayload(t *testing.T) {
	api, subSvc := newSubTestAPI(t)
	api.cfg.MihomoConfigDir = api.cfg.DataDir

	var rejected atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rejected.Load() {
			_, _ = w.Write([]byte(stubShareLink + "\n"))
			return
		}
		_, _ = w.Write([]byte(okShareLink + "\n"))
	}))
	defer ts.Close()

	sub := &services.Subscription{Name: "Rejected Sub", URL: ts.URL, Enabled: true, EnableMihomo: true}
	if err := subSvc.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	first := providerAdapterGet(t, api, ts.URL)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "1.2.3.4") {
		t.Fatalf("first response must carry working node: %d %s", first.Code, first.Body.String())
	}

	rejected.Store(true)
	second := providerAdapterGet(t, api, ts.URL)
	if second.Code != http.StatusOK {
		t.Fatalf("expected cached payload with 200, got %d: %s", second.Code, second.Body.String())
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("second response must equal cached first:\nfirst:  %q\nsecond: %q", first.Body.String(), second.Body.String())
	}
	if strings.Contains(second.Body.String(), "0.0.0.0") {
		t.Errorf("stub node must never reach Mihomo, got: %s", second.Body.String())
	}
	got := subSvc.Get(sub.ID)
	if got == nil || !got.DeviceRejected {
		t.Fatalf("subscription must be marked device_rejected, got %+v", got)
	}
	if got.LastError != "" {
		t.Errorf("last_error must stay empty on provider rejection, got %q", got.LastError)
	}
}
