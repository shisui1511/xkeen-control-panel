package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigServiceList(t *testing.T) {
	// Создаём временную директорию
	tmpDir := t.TempDir()
	
	// Создаём несколько тестовых JSON файлов
	testFiles := []string{"config1.json", "config2.json", "test.json"}
	for _, file := range testFiles {
		path := filepath.Join(tmpDir, file)
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}
	
	// Создаём также не-JSON файл
	if err := os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	svc := NewConfigService(tmpDir)
	files, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	
	// Должны получить только JSON файлы
	if len(files) != 3 {
		t.Errorf("Expected 3 JSON files, got %d", len(files))
	}
}

func TestConfigServiceReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	testData := []byte(`{"test": "data"}`)
	
	svc := NewConfigService(tmpDir)
	
	// Записываем данные
	err := svc.Save(testFile, testData)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	
	// Читаем данные
	data, err := svc.Read(testFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	
	if string(data) != string(testData) {
		t.Errorf("Expected %s, got %s", testData, data)
	}
}

func TestConfigServiceBackups(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	
	svc := NewConfigService(tmpDir)
	
	// Создаём первую версию
	err := svc.Save(testFile, []byte(`{"version": 1}`))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	
	// Создаём вторую версию (должен создаться backup)
	err = svc.Save(testFile, []byte(`{"version": 2}`))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	
	// Проверяем, что backup создан
	backups, err := svc.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	
	if len(backups) != 1 {
		t.Errorf("Expected 1 backup, got %d", len(backups))
	}
	
	// Проверяем содержимое backup
	if len(backups) > 0 {
		backupData, err := svc.Read(backups[0])
		if err != nil {
			t.Fatalf("Failed to read backup: %v", err)
		}
		
		if string(backupData) != `{"version": 1}` {
			t.Errorf("Backup contains wrong data: %s", backupData)
		}
	}
}

func TestConfigServiceBackupRotation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	
	svc := NewConfigService(tmpDir)
	
	// Создаём 7 версий (должно остаться только 5 последних)
	for i := 1; i <= 7; i++ {
		data := []byte(`{"version": ` + string(rune('0'+i)) + `}`)
		err := svc.Save(testFile, data)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}
	
	// Проверяем количество backups
	backups, err := svc.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	
	if len(backups) > 5 {
		t.Errorf("Expected max 5 backups, got %d", len(backups))
	}
}

func TestConfigServiceExists(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	
	svc := NewConfigService(tmpDir)
	
	// Файл не существует
	if svc.Exists(testFile) {
		t.Error("File should not exist")
	}
	
	// Создаём файл
	err := os.WriteFile(testFile, []byte("{}"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	
	// Файл существует
	if !svc.Exists(testFile) {
		t.Error("File should exist")
	}
}
