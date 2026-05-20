package cert

import (
	"crypto/tls"
	"path/filepath"
	"testing"
)

// TestTLSConfig_MinVersion verifies that LoadOrGenerate returns a tls.Config
// with MinVersion set to TLS 1.2, preventing downgrade attacks.
func TestTLSConfig_MinVersion(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	tlsCfg, err := LoadOrGenerate(certPath, keyPath, nil)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}

	if tlsCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsCfg.MinVersion, tls.VersionTLS12)
	}
}

// TestGenerateSelfSigned_ValidCert verifies that GenerateSelfSigned produces
// a valid PEM cert/key pair that can be loaded by tls.X509KeyPair.
func TestGenerateSelfSigned_ValidCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	if err := GenerateSelfSigned(certPath, keyPath, nil); err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}

	// Verify the key pair loads cleanly.
	tlsCfg, err := LoadOrGenerate(certPath, keyPath, nil)
	if err != nil {
		t.Fatalf("LoadOrGenerate after GenerateSelfSigned: %v", err)
	}

	if len(tlsCfg.Certificates) == 0 {
		t.Error("expected at least one certificate in tls.Config")
	}
}
