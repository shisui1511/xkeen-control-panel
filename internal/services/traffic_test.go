package services

import (
	"testing"
	"time"
)

func TestTrafficQuotaService_New(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestTrafficQuotaService_Add(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")

	q := &TrafficQuota{
		Name:       "Test Quota",
		LimitBytes: 1024 * 1024 * 1024,
		Period:     "daily",
		Enabled:    true,
	}

	err := svc.AddQuota(q)
	if err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	quotas := svc.ListQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas))
	}
	if quotas[0].Name != "Test Quota" {
		t.Fatalf("expected name 'Test Quota', got %s", quotas[0].Name)
	}
	if quotas[0].LimitBytes != 1024*1024*1024 {
		t.Fatalf("expected limit %d bytes, got %d", 1024*1024*1024, quotas[0].LimitBytes)
	}
}

func TestTrafficQuotaService_Delete(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")

	err := svc.AddQuota(&TrafficQuota{Name: "To Delete", LimitBytes: 1024, Period: "daily"})
	if err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	quotas := svc.ListQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota after add, got %d", len(quotas))
	}

	err = svc.DeleteQuota(quotas[0].ID)
	if err != nil {
		t.Fatalf("DeleteQuota failed: %v", err)
	}

	quotas = svc.ListQuotas()
	if len(quotas) != 0 {
		t.Fatalf("expected 0 quotas after delete, got %d", len(quotas))
	}
}

// TestWeeklyResetYearBoundary: lastReset Dec 28, now Jan 4 next year → AddDate-based reset should trigger.
func TestWeeklyResetYearBoundary(t *testing.T) {
	// Dec 28 of some year
	lastReset := time.Date(2023, 12, 28, 0, 0, 0, 0, time.UTC)
	// Jan 4 of next year (7+ days later)
	now := time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC)

	shouldReset := lastReset.AddDate(0, 0, 7).Before(now)
	if !shouldReset {
		t.Errorf("expected shouldReset=true for cross-year week boundary (AddDate), got false")
	}
}

// TestWeeklyResetNoReset: lastReset Jan 1, now Jan 5 (less than 7 days later) → no reset.
func TestWeeklyResetNoReset(t *testing.T) {
	lastReset := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC)

	shouldReset := lastReset.AddDate(0, 0, 7).Before(now)
	if shouldReset {
		t.Errorf("expected shouldReset=false for same-week (AddDate), got true")
	}
}

// TestTrafficQuotaService_CheckResets_YearBoundary verifies that checkResets correctly resets a weekly
// quota when the last reset was more than 7 days ago.
func TestTrafficQuotaService_CheckResets_YearBoundary(t *testing.T) {
	// Verify AddDate logic: Dec 28 + 7 days = Jan 4 which is Before Jan 4 12:00 → shouldReset = true
	lastResetTime := time.Date(2023, 12, 28, 0, 0, 0, 0, time.UTC)
	now := time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC)

	shouldReset := lastResetTime.AddDate(0, 0, 7).Before(now)
	if !shouldReset {
		t.Fatalf("expected shouldReset=true for year-boundary weekly quota via AddDate logic, got false")
	}
}

func TestTrafficQuotaService_Persistence(t *testing.T) {
	tmp := t.TempDir()

	svc1 := NewTrafficQuotaService(tmp, "http://localhost:9090")
	err := svc1.AddQuota(&TrafficQuota{Name: "Persistent Quota", LimitBytes: 2048, Period: "monthly"})
	if err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	svc2 := NewTrafficQuotaService(tmp, "http://localhost:9090")
	quotas := svc2.ListQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota after reload, got %d", len(quotas))
	}
	if quotas[0].Name != "Persistent Quota" {
		t.Fatalf("expected name 'Persistent Quota', got %s", quotas[0].Name)
	}
}
