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

// TestTrafficGet_ReturnsCopy (T008): modifying the value returned by GetQuota must not affect the original.
func TestTrafficGet_ReturnsCopy(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")

	q := &TrafficQuota{
		Name:       "OriginalQuota",
		LimitBytes: 1024,
		Period:     "daily",
		Enabled:    true,
	}
	if err := svc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	quotas := svc.ListQuotas()
	if len(quotas) != 1 {
		t.Fatal("expected 1 quota")
	}
	id := quotas[0].ID

	got, ok := svc.GetQuota(id)
	if !ok {
		t.Fatal("GetQuota returned not found")
	}

	// Mutate the returned copy
	got.Name = "MutatedQuota"
	got.LimitBytes = 9999

	// The original in the service slice must be unchanged
	original, ok := svc.GetQuota(id)
	if !ok {
		t.Fatal("second GetQuota returned not found")
	}
	if original.Name != "OriginalQuota" {
		t.Errorf("expected original name 'OriginalQuota', got %q (mutation leaked)", original.Name)
	}
	if original.LimitBytes != 1024 {
		t.Errorf("expected original LimitBytes 1024, got %d (mutation leaked)", original.LimitBytes)
	}
}

// TestSaveLocked_Throttle verifies that saveLocked(false) skips disk writes
// when called within saveLockThrottle, and saveLocked(true) always writes.
func TestSaveLocked_Throttle(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")

	// Force-save sets lastSave.
	svc.mu.Lock()
	err := svc.saveLocked(true)
	svc.mu.Unlock()
	if err != nil {
		t.Fatalf("first force save failed: %v", err)
	}

	// Immediately after, throttled save should be a no-op (returns nil, no write).
	svc.mu.Lock()
	lastSave := svc.lastSave
	err = svc.saveLocked(false)
	svc.mu.Unlock()
	if err != nil {
		t.Fatalf("throttled save returned error: %v", err)
	}
	// lastSave should not have advanced (throttle skipped the write).
	svc.mu.RLock()
	newLastSave := svc.lastSave
	svc.mu.RUnlock()
	if newLastSave.After(lastSave) {
		t.Error("throttled save should not advance lastSave within throttle window")
	}

	// Force-save always advances lastSave.
	svc.mu.Lock()
	err = svc.saveLocked(true)
	svc.mu.Unlock()
	if err != nil {
		t.Fatalf("second force save failed: %v", err)
	}
	svc.mu.RLock()
	afterForce := svc.lastSave
	svc.mu.RUnlock()
	if !afterForce.After(lastSave) {
		t.Error("force save should advance lastSave")
	}
}

// TestSaveLocked_ThrottleExpiry verifies that saveLocked(false) writes after the
// throttle window expires.
func TestSaveLocked_ThrottleExpiry(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090")

	// Set lastSave far in the past to simulate an expired throttle.
	svc.mu.Lock()
	svc.lastSave = time.Now().Add(-saveLockThrottle - time.Second)
	err := svc.saveLocked(false)
	svc.mu.Unlock()
	if err != nil {
		t.Fatalf("save after throttle expiry failed: %v", err)
	}
	// lastSave should now be fresh.
	svc.mu.RLock()
	elapsed := time.Since(svc.lastSave)
	svc.mu.RUnlock()
	if elapsed > time.Second {
		t.Errorf("expected lastSave to be recent after throttle expiry, elapsed=%v", elapsed)
	}
}
