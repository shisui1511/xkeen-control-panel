//go:build router

// router-area: server

package handlers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCountDirLinesCache(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	if err := os.WriteFile(p, []byte("1\n2\n3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := countDirLines(dir); got != 3 {
		t.Fatalf("first count = %d, want 3", got)
	}
	// Тот же размер и время изменения — берётся число из кэша.
	if got := countDirLines(dir); got != 3 {
		t.Fatalf("cached count = %d, want 3", got)
	}
	// Изменённый файл пересчитывается.
	if err := os.WriteFile(p, []byte("1\n2\n3\n4\n5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, future, future)
	if got := countDirLines(dir); got != 5 {
		t.Fatalf("recount = %d, want 5", got)
	}
}
