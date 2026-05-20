package services

import (
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
