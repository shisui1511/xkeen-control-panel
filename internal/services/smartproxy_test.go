package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

	schedule := make([][]bool, 7)
	for i := range schedule {
		schedule[i] = make([]bool, 24)
		schedule[i][12] = true // Active at 12:00
	}

	profile := Profile{
		Name:      "Test Profile",
		Enabled:   true,
		Mode:      ModeTimeBased,
		GroupName: "proxy-group",
		ProxyName: "proxy1",
		Schedule:  schedule,
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
	if !profiles[0].Schedule[0][12] {
		t.Fatal("expected schedule at [0][12] to be true")
	}
}

func TestSmartProxyService_Delete(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	profile := Profile{Name: "Delete Me", Enabled: true, Mode: ModeTimeBased}
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
		Mode:    ModeTimeBased,
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
	err := svc1.Add(&Profile{Name: "Persistent Profile", Enabled: true, Mode: ModeTimeBased})
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

func TestSmartProxyService_2DScheduling(t *testing.T) {
	var appliedGroup, appliedProxy string
	var applyCount int

	// Mock Mihomo server
	mux := http.NewServeMux()
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				appliedProxy = body["name"]
				// Extract group from path /proxies/{group}
				parts := r.URL.Path
				appliedGroup = parts[len("/proxies/"):]
				applyCount++
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, ts.URL)

	// Create 2D schedule: enable the current day & hour
	now := time.Now()
	day := int(now.Weekday())
	hour := now.Hour()

	schedule := make([][]bool, 7)
	for i := range schedule {
		schedule[i] = make([]bool, 24)
	}
	schedule[day][hour] = true // active right now

	profile := Profile{
		Name:      "Active Profile",
		Enabled:   true,
		Mode:      ModeTimeBased,
		GroupName: "my-group",
		ProxyName: "my-proxy",
		Schedule:  schedule,
	}

	if err := svc.Add(&profile); err != nil {
		t.Fatalf("Add active profile failed: %v", err)
	}

	// Run evaluateProfiles to trigger scheduler logic
	svc.evaluateProfiles()

	// Verify Mihomo was called
	if appliedGroup != "my-group" || appliedProxy != "my-proxy" {
		t.Fatalf("Expected proxy my-proxy applied to my-group, got group: %s, proxy: %s", appliedGroup, appliedProxy)
	}

	// Run it again — should not reapply since LastApplied is recent (< 5 min)
	svc.evaluateProfiles()
	if applyCount != 1 {
		t.Fatalf("Expected apply count to be 1, got %d", applyCount)
	}

	// Now check CurrentStatus
	status := svc.CurrentStatus()
	activeList := status["active"].([]Profile)
	if len(activeList) != 1 {
		t.Fatalf("Expected 1 active profile, got %d", len(activeList))
	}
	if activeList[0].Name != "Active Profile" {
		t.Fatalf("Expected active profile Name 'Active Profile', got %s", activeList[0].Name)
	}

	// Disable profile and verify it is not evaluated/active
	if err := svc.SetEnabled(profile.ID, false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	statusInactive := svc.CurrentStatus()
	activeListInactive := statusInactive["active"].([]Profile)
	if len(activeListInactive) != 0 {
		t.Fatalf("Expected 0 active profiles after disable, got %d", len(activeListInactive))
	}
}

func TestSmartProxyService_2DScheduling_NextStatus(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")

	now := time.Now()
	day := int(now.Weekday())
	nextHour := (now.Hour() + 1) % 24

	schedule := make([][]bool, 7)
	for i := range schedule {
		schedule[i] = make([]bool, 24)
	}
	// Make it active ONLY in the next hour (if nextHour is 0, it means tomorrow, but that's fine for testing the logic)
	schedule[day][nextHour] = true

	profile := Profile{
		Name:      "Next Hour Profile",
		Enabled:   true,
		Mode:      ModeTimeBased,
		GroupName: "my-group",
		ProxyName: "my-proxy",
		Schedule:  schedule,
	}

	if err := svc.Add(&profile); err != nil {
		t.Fatalf("Add profile failed: %v", err)
	}

	status := svc.CurrentStatus()
	activeList := status["active"].([]Profile)
	nextList := status["next"].([]Profile)

	if len(activeList) != 0 {
		t.Fatalf("Expected 0 active profiles, got %d", len(activeList))
	}

	// If nextHour > now.Hour(), it should be in the nextList
	if nextHour > now.Hour() {
		if len(nextList) != 1 {
			t.Fatalf("Expected 1 next profile, got %d", len(nextList))
		}
		if nextList[0].Name != "Next Hour Profile" {
			t.Fatalf("Expected next profile Name 'Next Hour Profile', got %s", nextList[0].Name)
		}
	}
}

func TestSmartProxyService_StartStop(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSmartProxyService(tmp, "http://localhost:9090")
	svc.Start()
	// Let it run briefly
	time.Sleep(10 * time.Millisecond)
	svc.Stop()
}
