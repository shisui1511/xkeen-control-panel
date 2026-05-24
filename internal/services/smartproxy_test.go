package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSmartProxyService_New(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestSmartProxyService_List_Empty(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")
	profiles := svc.List()
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestSmartProxyService_Add(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	profile := Profile{
		Name:       "Test Profile",
		Enabled:    true,
		Mode:       ModeTimeBased,
		GroupName:  "proxy-group",
		ProxyName:  "proxy1",
		StartTime:  "08:00",
		EndTime:    "20:00",
		DaysOfWeek: []int{1, 2, 3, 4, 5},
	}

	err := svc.Add(&profile)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	profiles := svc.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Name != "Test Profile" {
		t.Fatalf("expected Name 'Test Profile', got %s", profiles[0].Name)
	}
	if profiles[0].Mode != ModeTimeBased {
		t.Fatalf("expected mode 'time-based', got %s", profiles[0].Mode)
	}
}

func TestSmartProxyService_Delete(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	profile := Profile{Name: "Delete Me", Enabled: true, Mode: ModeFailover}
	err := svc.Add(&profile)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	profiles := svc.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after add, got %d", len(profiles))
	}

	err = svc.Delete(profiles[0].ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	profiles = svc.List()
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles after delete, got %d", len(profiles))
	}
}

func TestSmartProxyService_Update(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	profile := Profile{Name: "Old Name", Enabled: true, Mode: ModeTimeBased}
	err := svc.Add(&profile)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	profiles := svc.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after add, got %d", len(profiles))
	}

	updated := Profile{
		Name:    "New Name",
		Enabled: false,
		Mode:    ModeRoundRobin,
	}

	err = svc.Update(profiles[0].ID, &updated)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	profiles = svc.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after update, got %d", len(profiles))
	}
	if profiles[0].Name != "New Name" {
		t.Fatalf("expected Name 'New Name', got %s", profiles[0].Name)
	}
}

func TestSmartProxyService_Persistence(t *testing.T) {
	tmp := t.TempDir()

	svc1 := NewSmartProxyService(tmp, "http://localhost:9090")
	err := svc1.Add(&Profile{Name: "Persistent Profile", Enabled: true, Mode: ModeGeo})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	svc2 := NewSmartProxyService(tmp, "http://localhost:9090")
	profiles := svc2.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after reload, got %d", len(profiles))
	}
	if profiles[0].Name != "Persistent Profile" {
		t.Fatalf("expected Name 'Persistent Profile', got %s", profiles[0].Name)
	}
}

// TestSmartProxyGet_ReturnsCopy (T007): modifying the returned copy must not affect the original.
func TestSmartProxyGet_ReturnsCopy(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	p := Profile{
		Name:    "OriginalProfile",
		Enabled: true,
		Mode:    ModeTimeBased,
	}
	if err := svc.Add(&p); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	id := svc.List()[0].ID
	got := svc.Get(id)
	if got == nil {
		t.Fatal("Get returned nil")
	}

	// Mutate the returned copy
	got.Name = "MutatedProfile"

	// The original in the service slice must be unchanged
	original := svc.Get(id)
	if original == nil {
		t.Fatal("second Get returned nil")
	}
	if original.Name != "OriginalProfile" {
		t.Errorf("expected original name 'OriginalProfile', got %q (mutation leaked)", original.Name)
	}
}

// --- Pure-function tests (frozen clock) ---

// TestIsDayMatch verifies day-of-week matching with and without a schedule.
func TestIsDayMatch(t *testing.T) {
	tests := []struct {
		name string
		days []int
		day  int
		want bool
	}{
		{"empty list matches all", []int{}, 3, true},
		{"day in list", []int{1, 2, 3}, 2, true},
		{"day not in list", []int{1, 2, 3}, 5, false},
		{"sunday (0)", []int{0}, 0, true},
		{"saturday (6)", []int{6}, 6, true},
		{"single day miss", []int{4}, 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDayMatch(tc.days, tc.day); got != tc.want {
				t.Errorf("isDayMatch(%v, %d) = %v, want %v", tc.days, tc.day, got, tc.want)
			}
		})
	}
}

// TestIsTimeInRange covers normal ranges, midnight-crossing ranges, and boundary values.
func TestIsTimeInRange(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		end     string
		current string
		want    bool
	}{
		// Normal range
		{"inside normal range", "08:00", "20:00", "12:00", true},
		{"at start of range", "08:00", "20:00", "08:00", true},
		{"at end of range", "08:00", "20:00", "20:00", true},
		{"before start", "08:00", "20:00", "07:59", false},
		{"after end", "08:00", "20:00", "20:01", false},
		// Midnight-crossing range (22:00–06:00)
		{"midnight-crossing: after start", "22:00", "06:00", "23:00", true},
		{"midnight-crossing: before end", "22:00", "06:00", "05:00", true},
		{"midnight-crossing: at start", "22:00", "06:00", "22:00", true},
		{"midnight-crossing: at end", "22:00", "06:00", "06:00", true},
		{"midnight-crossing: middle of day (outside)", "22:00", "06:00", "14:00", false},
		{"midnight-crossing: just before start", "22:00", "06:00", "21:59", false},
		{"midnight-crossing: just after end", "22:00", "06:00", "06:01", false},
		// Full-day range
		{"full day start=end", "00:00", "23:59", "12:00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTimeInRange(tc.start, tc.end, tc.current); got != tc.want {
				t.Errorf("isTimeInRange(%q, %q, %q) = %v, want %v",
					tc.start, tc.end, tc.current, got, tc.want)
			}
		})
	}
}

// --- Failover recovery test via httptest ---

// TestEvaluateFailover_Recovery verifies that when the primary proxy recovers
// (delay <= threshold), the service switches back from the fallback proxy.
func TestEvaluateFailover_Recovery(t *testing.T) {
	// Mock Mihomo server: delay endpoint returns 50ms, PUT is a no-op.
	mux := http.NewServeMux()
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// delay endpoint
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 50})
			return
		}
		// PUT — proxy switch
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, ts.URL)

	profile := Profile{
		Name:                "FailoverTest",
		Enabled:             true,
		Mode:                ModeFailover,
		GroupName:           "proxy-group",
		ProxyName:           "primary",
		FallbackProxy:       "fallback",
		LatencyThreshold:    200,
		ConsecutiveFailures: 2,
		CurrentProxy:        "fallback", // simulating we are already on fallback
	}
	if err := svc.Add(&profile); err != nil {
		t.Fatalf("Add: %v", err)
	}

	id := svc.List()[0].ID

	// Run evaluateFailover once — primary is healthy (50ms <= 200ms threshold)
	p := svc.List()[0]
	svc.evaluateFailover(&p)

	// CurrentProxy should be reset (primary recovered)
	updated := svc.Get(id)
	if updated == nil {
		t.Fatal("Get returned nil after evaluateFailover")
	}
	if updated.CurrentProxy != "" {
		t.Errorf("expected CurrentProxy to be cleared after recovery, got %q", updated.CurrentProxy)
	}
	if updated.CurrentFailures != 0 {
		t.Errorf("expected CurrentFailures=0 after recovery, got %d", updated.CurrentFailures)
	}
}

// TestEvaluateFailover_Failover verifies that consecutive failures trigger a switch to the fallback proxy.
func TestEvaluateFailover_Failover(t *testing.T) {
	// Mock Mihomo: delay returns 9999ms (above threshold), PUT accepted.
	mux := http.NewServeMux()
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 9999})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, ts.URL)

	profile := Profile{
		Name:                "FailoverTrigger",
		Enabled:             true,
		Mode:                ModeFailover,
		GroupName:           "proxy-group",
		ProxyName:           "primary",
		FallbackProxy:       "fallback",
		LatencyThreshold:    200,
		ConsecutiveFailures: 2,
	}
	if err := svc.Add(&profile); err != nil {
		t.Fatalf("Add: %v", err)
	}

	id := svc.List()[0].ID

	// First failure — not yet at threshold
	p := svc.List()[0]
	svc.evaluateFailover(&p)
	after1 := svc.Get(id)
	if after1.CurrentProxy != "" {
		t.Errorf("after 1 failure: expected no failover yet, got CurrentProxy=%q", after1.CurrentProxy)
	}

	// Second failure — should trigger failover
	p = svc.List()[0]
	svc.evaluateFailover(&p)
	after2 := svc.Get(id)
	if after2.CurrentProxy != "fallback" {
		t.Errorf("after 2 failures: expected CurrentProxy=%q, got %q", "fallback", after2.CurrentProxy)
	}
}
