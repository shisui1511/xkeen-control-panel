package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrafficQuotaService_New(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestTrafficQuotaService_Add(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

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
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

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

	svc1 := NewTrafficQuotaService(tmp, "http://localhost:9090", "")
	err := svc1.AddQuota(&TrafficQuota{Name: "Persistent Quota", LimitBytes: 2048, Period: "monthly"})
	if err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	svc2 := NewTrafficQuotaService(tmp, "http://localhost:9090", "")
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
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

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
	if got.Name != "MutatedQuota" || got.LimitBytes != 9999 {
		t.Fatal("failed to mutate local copy")
	}

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
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

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

// --- Delta calculation ---

// TestProcessConnSnapshot_Delta verifies that processConnSnapshot correctly
// calculates delta bytes for existing connections and treats new ones as full deltas.
func TestProcessConnSnapshot_Delta(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	// Seed the tracker with a connection that already has some bytes.
	svc.connectionTracker.Store("conn1", connStats{Upload: 100, Download: 200})

	// Snapshot: conn1 increased, conn2 is new.
	snapshot := []mihomoConn{
		{ID: "conn1", Chains: []string{"proxy-a"}, Upload: 150, Download: 300},
		{ID: "conn2", Chains: []string{"proxy-b"}, Upload: 50, Download: 80},
	}
	svc.processConnSnapshot(snapshot)

	svc.mu.RLock()
	statsA := svc.proxyStats["proxy-a"]
	statsB := svc.proxyStats["proxy-b"]
	svc.mu.RUnlock()

	if statsA == nil {
		t.Fatal("expected proxyStats for proxy-a")
	}
	// delta for conn1: upload=50, download=100
	if statsA.UploadBytes != 50 {
		t.Errorf("proxy-a UploadBytes: want 50, got %d", statsA.UploadBytes)
	}
	if statsA.DownloadBytes != 100 {
		t.Errorf("proxy-a DownloadBytes: want 100, got %d", statsA.DownloadBytes)
	}
	if statsA.TotalBytes != 150 {
		t.Errorf("proxy-a TotalBytes: want 150, got %d", statsA.TotalBytes)
	}

	if statsB == nil {
		t.Fatal("expected proxyStats for proxy-b")
	}
	// New connection: full bytes counted as delta.
	if statsB.UploadBytes != 50 {
		t.Errorf("proxy-b UploadBytes: want 50, got %d", statsB.UploadBytes)
	}
	if statsB.DownloadBytes != 80 {
		t.Errorf("proxy-b DownloadBytes: want 80, got %d", statsB.DownloadBytes)
	}
}

// TestProcessConnSnapshot_ClosedConnectionCleanup verifies that connections not in
// the latest snapshot are removed from the tracker.
func TestProcessConnSnapshot_ClosedConnectionCleanup(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	svc.connectionTracker.Store("old-conn", connStats{Upload: 10, Download: 20})

	// Snapshot without old-conn → it should be removed.
	svc.processConnSnapshot([]mihomoConn{})

	if _, ok := svc.connectionTracker.Load("old-conn"); ok {
		t.Error("expected old-conn to be removed from tracker after closure")
	}
}

// TestProcessConnSnapshot_NegativeDeltaIgnored verifies that negative deltas
// (counter wraparound / reconnect) are clamped to zero.
func TestProcessConnSnapshot_NegativeDeltaIgnored(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	svc.connectionTracker.Store("conn1", connStats{Upload: 500, Download: 500})

	// Upload/Download are lower than the last seen values (counter reset).
	snapshot := []mihomoConn{
		{ID: "conn1", Chains: []string{"proxy-a"}, Upload: 100, Download: 100},
	}
	svc.processConnSnapshot(snapshot)

	svc.mu.RLock()
	stats := svc.proxyStats["proxy-a"]
	svc.mu.RUnlock()

	// Stats entry should not have been created (delta was 0 after clamping).
	if stats != nil && (stats.UploadBytes != 0 || stats.DownloadBytes != 0) {
		t.Errorf("expected 0 bytes from negative delta, got up=%d down=%d",
			stats.UploadBytes, stats.DownloadBytes)
	}
}

// --- Period boundary tests ---

// TestCheckResets_DailyBoundary verifies that a daily quota resets when LastReset is yesterday.
func TestCheckResets_DailyBoundary(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	yesterday := time.Now().AddDate(0, 0, -1)
	quota := &TrafficQuota{
		Name:         "DailyQuota",
		TargetType:   "global",
		LimitBytes:   1024,
		Period:       "daily",
		Enabled:      true,
		CurrentBytes: 999,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	// Backdate LastReset to yesterday via UpdateQuota.
	id := svc.ListQuotas()[0].ID
	updated := svc.ListQuotas()[0]
	updated.LastReset = yesterday.Unix()
	if err := svc.UpdateQuota(id, &updated); err != nil {
		t.Fatalf("UpdateQuota: %v", err)
	}

	svc.checkResets()

	quotas := svc.ListQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas))
	}
	if quotas[0].CurrentBytes != 0 {
		t.Errorf("expected CurrentBytes=0 after daily reset, got %d", quotas[0].CurrentBytes)
	}
	if quotas[0].LastReset <= yesterday.Unix() {
		t.Errorf("expected LastReset to advance after reset")
	}
}

// TestCheckResets_MonthlyBoundary verifies that a monthly quota resets when LastReset is last month.
func TestCheckResets_MonthlyBoundary(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	lastMonth := time.Now().AddDate(0, 0, -45)
	quota := &TrafficQuota{
		Name:         "MonthlyQuota",
		TargetType:   "global",
		LimitBytes:   10_000_000,
		Period:       "monthly",
		Enabled:      true,
		CurrentBytes: 5_000_000,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	id := svc.ListQuotas()[0].ID
	updated := svc.ListQuotas()[0]
	updated.LastReset = lastMonth.Unix()
	if err := svc.UpdateQuota(id, &updated); err != nil {
		t.Fatalf("UpdateQuota: %v", err)
	}

	svc.checkResets()

	quotas := svc.ListQuotas()
	if quotas[0].CurrentBytes != 0 {
		t.Errorf("expected CurrentBytes=0 after monthly reset, got %d", quotas[0].CurrentBytes)
	}
}

// TestCheckResets_NoResetWithinPeriod verifies that a quota within its period is not reset.
func TestCheckResets_NoResetWithinPeriod(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	quota := &TrafficQuota{
		Name:         "FreshQuota",
		TargetType:   "global",
		LimitBytes:   1024,
		Period:       "daily",
		Enabled:      true,
		CurrentBytes: 100,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	// Manually bump CurrentBytes to 100 (AddQuota sets it to 0).
	id := svc.ListQuotas()[0].ID
	current := svc.ListQuotas()[0]
	current.CurrentBytes = 100
	// LastReset defaults to now (same day) — no reset should trigger.
	if err := svc.UpdateQuota(id, &current); err != nil {
		t.Fatalf("UpdateQuota: %v", err)
	}

	svc.checkResets()

	quotas := svc.ListQuotas()
	if quotas[0].CurrentBytes != 100 {
		t.Errorf("expected CurrentBytes=100 (no reset), got %d", quotas[0].CurrentBytes)
	}
}

// TestCheckResets_ProxyStatsReset verifies that proxyStats for the target proxy
// are zeroed when the quota resets.
func TestCheckResets_ProxyStatsReset(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	// Add and backdate the quota.
	quota := &TrafficQuota{
		Name:       "ProxyQuota",
		TargetType: "proxy",
		TargetID:   "proxy-x",
		LimitBytes: 2000,
		Period:     "daily",
		Enabled:    true,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}
	id := svc.ListQuotas()[0].ID
	backdated := svc.ListQuotas()[0]
	backdated.LastReset = time.Now().AddDate(0, 0, -1).Unix()
	if err := svc.UpdateQuota(id, &backdated); err != nil {
		t.Fatalf("UpdateQuota: %v", err)
	}

	// Manually inject proxy stats.
	svc.mu.Lock()
	svc.proxyStats["proxy-x"] = &ProxyTraffic{
		ProxyName:     "proxy-x",
		UploadBytes:   500,
		DownloadBytes: 500,
		TotalBytes:    1000,
	}
	svc.mu.Unlock()

	svc.checkResets()

	svc.mu.RLock()
	stat := svc.proxyStats["proxy-x"]
	svc.mu.RUnlock()

	if stat == nil {
		t.Fatal("expected proxyStats entry to still exist")
	}
	if stat.TotalBytes != 0 {
		t.Errorf("expected proxyStats zeroed after reset, got TotalBytes=%d", stat.TotalBytes)
	}
}

// --- Alert generation tests ---

// TestCheckQuotas_CriticalAlert verifies that a critical alert is generated when usage >= 100%.
func TestCheckQuotas_CriticalAlert(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	svc.mu.Lock()
	svc.proxyStats["proxy-z"] = &ProxyTraffic{
		ProxyName:  "proxy-z",
		TotalBytes: 1200, // over limit
	}
	svc.mu.Unlock()

	quota := &TrafficQuota{
		Name:       "OverLimit",
		TargetType: "proxy",
		TargetID:   "proxy-z",
		LimitBytes: 1000,
		Period:     "daily",
		Enabled:    true,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	svc.checkQuotas()

	alerts := svc.GetAlerts()
	if len(alerts) == 0 {
		t.Fatal("expected at least one alert, got none")
	}
	var found bool
	for _, a := range alerts {
		if a.Severity == "critical" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected critical alert, got %+v", alerts)
	}
}

// TestCheckQuotas_WarningAlert verifies that a warning alert fires when usage crosses alertThreshold.
func TestCheckQuotas_WarningAlert(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	svc.mu.Lock()
	svc.proxyStats["proxy-w"] = &ProxyTraffic{
		ProxyName:  "proxy-w",
		TotalBytes: 850, // 85% of 1000
	}
	svc.mu.Unlock()

	quota := &TrafficQuota{
		Name:           "WarnQuota",
		TargetType:     "proxy",
		TargetID:       "proxy-w",
		LimitBytes:     1000,
		AlertThreshold: 80, // warn at 80%
		Period:         "daily",
		Enabled:        true,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	svc.checkQuotas()

	alerts := svc.GetAlerts()
	if len(alerts) == 0 {
		t.Fatal("expected at least one alert, got none")
	}
	if alerts[0].Severity != "warning" {
		t.Errorf("expected warning alert, got severity=%q", alerts[0].Severity)
	}
}

// TestCheckQuotas_NoAlertBelowThreshold verifies that no alert fires when usage is below threshold.
func TestCheckQuotas_NoAlertBelowThreshold(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	svc.mu.Lock()
	svc.proxyStats["proxy-ok"] = &ProxyTraffic{
		ProxyName:  "proxy-ok",
		TotalBytes: 500, // 50% of 1000, threshold at 80%
	}
	svc.mu.Unlock()

	quota := &TrafficQuota{
		Name:           "OkQuota",
		TargetType:     "proxy",
		TargetID:       "proxy-ok",
		LimitBytes:     1000,
		AlertThreshold: 80,
		Period:         "daily",
		Enabled:        true,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	svc.checkQuotas()

	alerts := svc.GetAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected no alerts at 50%% usage, got %d: %+v", len(alerts), alerts)
	}
}

// TestSaveLocked_ThrottleExpiry verifies that saveLocked(false) writes after the
// throttle window expires.
func TestSaveLocked_ThrottleExpiry(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

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

func TestTrafficPeaks_Calculation(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	// 1. Initial values are 0
	if svc.peaks.PeakHourUp != 0 || svc.peaks.PeakHourDown != 0 {
		t.Fatal("expected initial peaks to be 0")
	}

	// 2. Feed growing speeds
	svc.processTrafficSnapshot(100, 200)
	if svc.peaks.PeakHourUp != 100 || svc.peaks.PeakHourDown != 200 {
		t.Fatalf("expected peaks to update to 100/200, got %d/%d", svc.peaks.PeakHourUp, svc.peaks.PeakHourDown)
	}

	// 3. Feed lower speeds, peaks should stay the same
	svc.processTrafficSnapshot(50, 100)
	if svc.peaks.PeakHourUp != 100 || svc.peaks.PeakHourDown != 200 {
		t.Fatalf("expected peaks to remain 100/200, got %d/%d", svc.peaks.PeakHourUp, svc.peaks.PeakHourDown)
	}

	// 4. Feed higher speeds, peaks should increase
	svc.processTrafficSnapshot(150, 250)
	if svc.peaks.PeakHourUp != 150 || svc.peaks.PeakHourDown != 250 {
		t.Fatalf("expected peaks to update to 150/250, got %d/%d", svc.peaks.PeakHourUp, svc.peaks.PeakHourDown)
	}
}

func TestTrafficPeaks_ResetOnBoundaries(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	// Set initial peaks and starts
	now := time.Now()
	svc.peaks = TrafficPeaks{
		PeakHourUp:   100,
		PeakHourDown: 100,
		PeakDayUp:    100,
		PeakDayDown:  100,
		PeakWeekUp:   100,
		PeakWeekDown: 100,
		HourStart:    now.Add(-2 * time.Hour).Unix(),
		DayStart:     now.Add(-25 * time.Hour).Unix(),
		WeekStart:    now.Add(-10 * 24 * time.Hour).Unix(),
	}

	// Process snapshot, which triggers calendar checks
	svc.processTrafficSnapshot(10, 10)

	// Since start hours/days/weeks are old, peaks should have reset and then updated to 10/10
	if svc.peaks.PeakHourUp != 10 || svc.peaks.PeakHourDown != 10 {
		t.Errorf("expected hour peaks to reset to 10, got up=%d, down=%d", svc.peaks.PeakHourUp, svc.peaks.PeakHourDown)
	}
	if svc.peaks.PeakDayUp != 10 || svc.peaks.PeakDayDown != 10 {
		t.Errorf("expected day peaks to reset to 10, got up=%d, down=%d", svc.peaks.PeakDayUp, svc.peaks.PeakDayDown)
	}
	if svc.peaks.PeakWeekUp != 10 || svc.peaks.PeakWeekDown != 10 {
		t.Errorf("expected week peaks to reset to 10, got up=%d, down=%d", svc.peaks.PeakWeekUp, svc.peaks.PeakWeekDown)
	}
}

func TestTraffic_ResetStats(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	// Set some stats and peaks
	svc.mu.Lock()
	svc.proxyStats["test-proxy"] = &ProxyTraffic{
		ProxyName:   "test-proxy",
		UploadBytes: 1000,
	}
	svc.peaks = TrafficPeaks{
		PeakHourUp: 500,
	}
	svc.mu.Unlock()

	// Add a quota with non-zero CurrentBytes
	quota := &TrafficQuota{
		Name:         "QuotaToReset",
		LimitBytes:   10000,
		CurrentBytes: 5000,
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota: %v", err)
	}

	// Run ResetStats
	if err := svc.ResetStats(); err != nil {
		t.Fatalf("ResetStats failed: %v", err)
	}

	// Verify stats, peaks, and quota CurrentBytes are reset
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.proxyStats) != 0 {
		t.Errorf("expected proxy stats to be empty, got %d", len(svc.proxyStats))
	}
	if svc.peaks.PeakHourUp != 0 {
		t.Errorf("expected peaks to be reset, got %d", svc.peaks.PeakHourUp)
	}
	if svc.quotas[0].CurrentBytes != 0 {
		t.Errorf("expected quota CurrentBytes to be 0, got %d", svc.quotas[0].CurrentBytes)
	}
}

func TestTraffic_TCPUDPAggregation(t *testing.T) {
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, "http://localhost:9090", "")

	snapshot := []mihomoConn{
		{
			ID: "conn1",
			Chains: []string{"proxy-a"},
			Metadata: mihomoConnMetadata{
				Network: "TCP",
			},
		},
		{
			ID: "conn2",
			Chains: []string{"proxy-a"},
			Metadata: mihomoConnMetadata{
				Network: "udp",
			},
		},
		{
			ID: "conn3",
			Chains: []string{"proxy-b"},
			Metadata: mihomoConnMetadata{
				Network: "TCP",
			},
		},
	}

	svc.processConnSnapshot(snapshot)

	if svc.activeConnsCount != 3 {
		t.Errorf("expected activeConnsCount=3, got %d", svc.activeConnsCount)
	}
	if svc.tcpConnsCount != 2 {
		t.Errorf("expected tcpConnsCount=2, got %d", svc.tcpConnsCount)
	}
	if svc.udpConnsCount != 1 {
		t.Errorf("expected udpConnsCount=1, got %d", svc.udpConnsCount)
	}
}

func TestTrafficQuotaService_BlockAndRedirect(t *testing.T) {
	var requestCount int
	var lastMethod, lastPath string
	var lastBody map[string]string

	// 1. Setup mock Mihomo server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		lastMethod = r.Method
		lastPath = r.URL.Path

		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/proxies/") {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				lastBody = body
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method == http.MethodGet && r.URL.Path == "/proxies" {
			proxiesJSON := `{
				"proxies": {
					"GLOBAL": {
						"name": "GLOBAL",
						"type": "Selector",
						"now": "US-Group",
						"all": ["US-Group", "DIRECT", "REJECT"]
					},
					"US-Group": {
						"name": "US-Group",
						"type": "Selector",
						"now": "us-node-1",
						"all": ["us-node-1", "us-node-2", "DIRECT", "REJECT"]
					}
				}
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(proxiesJSON))
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// 2. Initialize Service with Mock Server URL
	tmp := t.TempDir()
	svc := NewTrafficQuotaService(tmp, ts.URL, "test-secret")

	// 3. Add Quota (redirect_direct, target: US-Group)
	quota := &TrafficQuota{
		ID:         "quota-us",
		Name:       "US Quota",
		TargetType: "proxy",
		TargetID:   "US-Group",
		LimitBytes: 1000,
		Enabled:    true,
		Action:     "redirect_direct",
	}
	if err := svc.AddQuota(quota); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	// 4. Simulate usage exceeding the limit (1200 bytes)
	svc.mu.Lock()
	svc.proxyStats["US-Group"] = &ProxyTraffic{
		ProxyName:  "US-Group",
		TotalBytes: 1200,
	}
	svc.mu.Unlock()

	// 5. Run checkQuotas()
	svc.checkQuotas()

	// Verify group switch API request was sent to Mihomo
	if lastMethod != http.MethodPut || lastPath != "/proxies/US-Group" {
		t.Errorf("expected PUT to /proxies/US-Group, got %s %s", lastMethod, lastPath)
	}
	if lastBody["name"] != "DIRECT" {
		t.Errorf("expected switch to DIRECT, got %s", lastBody["name"])
	}

	// Verify original proxy is saved in blockedProxies
	svc.mu.RLock()
	orig := svc.blockedProxies["US-Group"]
	svc.mu.RUnlock()
	if orig != "us-node-1" {
		t.Errorf("expected original proxy to be saved as us-node-1, got %s", orig)
	}

	// 6. Reset Quota and run checkQuotas() again -> should restore original proxy
	if err := svc.ResetQuota("quota-us"); err != nil {
		t.Fatalf("ResetQuota failed: %v", err)
	}

	svc.mu.Lock()
	svc.proxyStats["US-Group"].TotalBytes = 0
	svc.mu.Unlock()

	// Re-mock proxies with current status (which was DIRECT after block)
	// so that checkQuotas sees the current state in Mihomo
	ts.Close() // close previous server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/proxies/") {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				lastBody = body
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/proxies" {
			proxiesJSON := `{
				"proxies": {
					"GLOBAL": {
						"name": "GLOBAL",
						"type": "Selector",
						"now": "US-Group",
						"all": ["US-Group", "DIRECT", "REJECT"]
					},
					"US-Group": {
						"name": "US-Group",
						"type": "Selector",
						"now": "DIRECT",
						"all": ["us-node-1", "us-node-2", "DIRECT", "REJECT"]
					}
				}
			}`
			w.Write([]byte(proxiesJSON))
			return
		}
	}))
	defer ts.Close()
	svc.mihomoURL = ts.URL

	svc.checkQuotas()

	// Verify restore API request was sent
	if lastMethod != http.MethodPut || lastPath != "/proxies/US-Group" {
		t.Errorf("expected restore PUT to /proxies/US-Group, got %s %s", lastMethod, lastPath)
	}
	if lastBody["name"] != "us-node-1" {
		t.Errorf("expected restore to us-node-1, got %s", lastBody["name"])
	}

	// Verify original proxy was removed from blockedProxies
	svc.mu.RLock()
	_, saved := svc.blockedProxies["US-Group"]
	svc.mu.RUnlock()
	if saved {
		t.Error("expected US-Group to be removed from blockedProxies")
	}
}
