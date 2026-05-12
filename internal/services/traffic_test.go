package services

import (
	"testing"
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
