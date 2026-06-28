package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanOrphanedSubscriptions(t *testing.T) {
	// Создаем временную директорию
	tempDir, err := os.MkdirTemp("", "sub-cleanup-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Создаем структуру SubscriptionService с временной директорией в качестве dataDir
	s := &SubscriptionService{
		dataDir: tempDir,
		// Активная подписка
		subscriptions: []Subscription{
			{ID: "active-sub"},
		},
	}

	// Папка для файлов подписок
	subDir := filepath.Join(tempDir, "subscriptions")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subscriptions dir: %v", err)
	}

	// 1. Создаем файлы для активной подписки (должны остаться)
	activeFiles := []string{
		"sub_active-sub_raw.txt",
		"sub_active-sub_headers.json",
		"sub_active-sub_parse_report.json",
	}
	// 2. Создаем файлы для осиротевшей подписки, но свежие (должны остаться)
	freshOrphanFiles := []string{
		"sub_fresh_orphan_raw.txt",
		"sub_fresh_orphan_headers.json",
		"sub_fresh_orphan_parse_report.json",
	}
	// 3. Создаем файлы для осиротевшей подписки, старые (должны быть удалены)
	oldOrphanFiles := []string{
		"sub_old_orphan_raw.txt",
		"sub_old_orphan_headers.json",
		"sub_old_orphan_parse_report.json",
	}

	now := time.Now()
	oldTime := now.Add(-8 * 24 * time.Hour) // старше 7 дней

	for _, f := range activeFiles {
		p := filepath.Join(subDir, f)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, oldTime, oldTime) // делаем старыми для чистоты теста
	}

	for _, f := range freshOrphanFiles {
		p := filepath.Join(subDir, f)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for _, f := range oldOrphanFiles {
		p := filepath.Join(subDir, f)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, oldTime, oldTime) // делаем старыми
	}

	// Запускаем очистку
	s.CleanOrphanedSubscriptions()

	// Проверяем результаты
	// Активная подписка должна остаться
	for _, f := range activeFiles {
		p := filepath.Join(subDir, f)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("active subscription file %s was incorrectly deleted", f)
		}
	}

	// Свежая осиротевшая подписка должна остаться
	for _, f := range freshOrphanFiles {
		p := filepath.Join(subDir, f)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("fresh orphan subscription file %s was incorrectly deleted", f)
		}
	}

	// Старая осиротевшая подписка должна быть удалена
	for _, f := range oldOrphanFiles {
		p := filepath.Join(subDir, f)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("old orphan subscription file %s was not deleted", f)
		}
	}
}