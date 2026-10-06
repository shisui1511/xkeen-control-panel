package configlayer

import (
	"os"
	"path/filepath"
	"testing"
)

// writeXrayFile кладёт файл в корень Xray временного каталога.
func writeXrayFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestDrift_TracerOKAndModified(t *testing.T) {
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	const name = "04_outbounds.xcp-diag.tail.json"
	body := []byte("{}\n")
	writeXrayFile(t, roots.Xray, name, string(body))

	entry := ManifestEntry{
		Kernel:  KernelXray,
		RelPath: name,
		Kind:    KindXrayJSON,
		Hash:    HashContent(body),
		Status:  StatusManaged,
	}

	got := CheckEntry(roots, entry)
	if got.State != StateOK {
		t.Fatalf("State = %q, want %q", got.State, StateOK)
	}
	if got.AbsPath != filepath.Join(roots.Xray, name) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, filepath.Join(roots.Xray, name))
	}

	writeXrayFile(t, roots.Xray, name, "{\"changed\":true}\n")
	got = CheckEntry(roots, entry)
	if got.State != StateDriftModified {
		t.Fatalf("State after edit = %q, want %q", got.State, StateDriftModified)
	}
	if got.ActualHash == "" || got.ActualHash == entry.Hash {
		t.Fatalf("ActualHash = %q, must differ from manifest hash %q", got.ActualHash, entry.Hash)
	}
}
