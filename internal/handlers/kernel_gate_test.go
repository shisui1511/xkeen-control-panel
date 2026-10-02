package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// newKernelGateAPI собирает API с KernelService и подменным источником
// процессов: running — имена запущенных ядер.
func newKernelGateAPI(t *testing.T, running ...string) *API {
	t.Helper()
	svc := services.NewKernelService(t.TempDir())
	svc.SetProcessStatesSource(func() []services.KernelProcessState {
		states := []services.KernelProcessState{
			{Name: "xray", Status: "stopped"},
			{Name: "mihomo", Status: "stopped"},
		}
		for i := range states {
			for _, name := range running {
				if states[i].Name == name {
					states[i].Status = "running"
					states[i].PID = 100 + i
				}
			}
		}
		return states
	})
	api := &API{cfg: &config.Config{}}
	api.SetKernelService(svc)
	return api
}

// gateCall вызывает обёрнутый обработчик и сообщает, дошёл ли запрос до него.
func gateCall(t *testing.T, api *API, pattern string) (rr *httptest.ResponseRecorder, called bool) {
	t.Helper()
	h := api.KernelRouteWrapper(pattern, func(w http.ResponseWriter, r *http.Request) {
		called = true
		JSONSuccess(w, "ok")
	})
	rr = httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, pattern, nil))
	return rr, called
}

// assertKernelInactive проверяет конверт 409 и что обработчик не вызывался.
func assertKernelInactive(t *testing.T, rr *httptest.ResponseRecorder, called bool, required, active string) {
	t.Helper()
	if called {
		t.Fatal("обработчик вызван, ожидался отказ гейта")
	}
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело 409 не JSON: %v: %s", err, rr.Body.String())
	}
	if body["success"] != false {
		t.Errorf("success = %v, want false", body["success"])
	}
	if body["code"] != "kernel_inactive" {
		t.Errorf("code = %v, want kernel_inactive", body["code"])
	}
	if body["required"] != required {
		t.Errorf("required = %v, want %s", body["required"], required)
	}
	if body["active"] != active {
		t.Errorf("active = %v, want %s", body["active"], active)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Error("error пуст, ожидался переведённый текст")
	}
}

func TestKernelGate_LiveRejectsWrongKernel(t *testing.T) {
	api := newKernelGateAPI(t, "xray")
	rr, called := gateCall(t, api, "/api/mihomo/proxy/")
	assertKernelInactive(t, rr, called, "mihomo", "xray")

	api = newKernelGateAPI(t, "mihomo")
	rr, called = gateCall(t, api, "/api/xray/stats")
	assertKernelInactive(t, rr, called, "xray", "mihomo")
}

func TestKernelGate_LiveConflict(t *testing.T) {
	api := newKernelGateAPI(t, "xray", "mihomo")
	rr, called := gateCall(t, api, "/api/mihomo/proxy/")
	assertKernelInactive(t, rr, called, "mihomo", "both")

	rr, called = gateCall(t, api, "/api/xray/stats")
	assertKernelInactive(t, rr, called, "xray", "both")
}

func TestKernelGate_LiveNone(t *testing.T) {
	api := newKernelGateAPI(t)
	rr, called := gateCall(t, api, "/api/traffic/stats")
	assertKernelInactive(t, rr, called, "mihomo", "none")
}

func TestKernelGate_LivePassesActive(t *testing.T) {
	api := newKernelGateAPI(t, "mihomo")
	rr, called := gateCall(t, api, "/api/mihomo/proxy/")
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d, ожидался проход к обработчику", called, rr.Code)
	}

	api = newKernelGateAPI(t, "xray")
	if rr, called = gateCall(t, api, "/api/xray/restart-logger"); !called || rr.Code != http.StatusOK {
		t.Fatalf("xray: called=%v status=%d, ожидался проход", called, rr.Code)
	}
}

// Ядро остановлено, но настроено (name_client): запрос доходит до обработчика,
// тот отвечает своей прежней ошибкой «не запущен».
func TestKernelGate_LiveStoppedConfiguredPasses(t *testing.T) {
	api := newKernelGateAPI(t)
	api.kernelSvc.SetActiveFallbacks(nil, func() string { return "mihomo" })
	rr, called := gateCall(t, api, "/api/mihomo/dns/query")
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d, ожидался проход при настроенном ядре", called, rr.Code)
	}
}

func TestKernelGate_DataNeverGated(t *testing.T) {
	patterns := []string{"/api/dat/list", "/api/outbound/parse", "/api/mihomo/profiles/activate", "/api/traffic/quotas", "/api/proxy-providers"}
	for _, running := range [][]string{{"xray"}, {"xray", "mihomo"}, {}} {
		api := newKernelGateAPI(t, running...)
		for _, pattern := range patterns {
			rr, called := gateCall(t, api, pattern)
			if !called || rr.Code != http.StatusOK {
				t.Errorf("running=%v %s: called=%v status=%d, маршрут данных не гейтится", running, pattern, called, rr.Code)
			}
		}
	}
}

func TestKernelGate_UngatedPatternUntouched(t *testing.T) {
	api := newKernelGateAPI(t, "xray", "mihomo")
	rr, called := gateCall(t, api, "/api/config/list")
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d, маршрут вне префиксов не трогается", called, rr.Code)
	}
}

func TestKernelGate_NoKernelServicePasses(t *testing.T) {
	api := &API{cfg: &config.Config{}}
	rr, called := gateCall(t, api, "/api/mihomo/proxy/")
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d, без kernelSvc гейт не действует", called, rr.Code)
	}
}

func TestKernelGate_UnknownGatedPatternPanics(t *testing.T) {
	api := newKernelGateAPI(t)
	const pattern = "/api/mihomo/new-endpoint"
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("регистрация маршрута без записи в таблице не паникует")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, pattern) {
			t.Errorf("текст паники %q не содержит шаблон %q", msg, pattern)
		}
	}()
	api.KernelRouteWrapper(pattern, func(http.ResponseWriter, *http.Request) {})
}

func TestKernelGate_TableInvariants(t *testing.T) {
	api := newKernelGateAPI(t)
	prefixes := KernelGatedPrefixes()
	if len(prefixes) == 0 {
		t.Fatal("список префиксов пуст")
	}
	for pattern, p := range kernelRoutePolicies {
		if p.Mode != GateLive && p.Mode != GateData {
			t.Errorf("%s: режим %q, допустимы live/data", pattern, p.Mode)
		}
		if p.Mode == GateLive && p.Kernel != "xray" && p.Kernel != "mihomo" {
			t.Errorf("%s: у live ядро %q, допустимы xray/mihomo", pattern, p.Kernel)
		}
		if strings.TrimSpace(p.Reason) == "" {
			t.Errorf("%s: пустая причина", pattern)
		}
		if !isKernelGatedPattern(pattern) {
			t.Errorf("%s: ключ не начинается ни с одного из префиксов гейта", pattern)
		}
		got, ok := KernelRoutePolicyFor(pattern)
		if !ok || got != p {
			t.Errorf("%s: KernelRoutePolicyFor вернул %+v ok=%v", pattern, got, ok)
		}
		// Обёртка для каждого ключа таблицы строится без паники.
		_ = api.KernelRouteWrapper(pattern, func(http.ResponseWriter, *http.Request) {})
	}
}

func TestKernelGate_GatedPrefixesIsCopy(t *testing.T) {
	a := KernelGatedPrefixes()
	a[0] = "/changed"
	if KernelGatedPrefixes()[0] == "/changed" {
		t.Error("KernelGatedPrefixes отдаёт внутренний срез, ожидалась копия")
	}
}
