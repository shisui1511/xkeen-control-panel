package services

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestGenerateRealityKeypair(t *testing.T) {
	kp, err := GenerateRealityKeypair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if kp.PrivateKey == "" {
		t.Errorf("expected non-empty private key")
	}
	if kp.PublicKey == "" {
		t.Errorf("expected non-empty public key")
	}
	if len(kp.ShortID) != 16 {
		t.Errorf("expected 16-hex-char short ID, got length %d: %s", len(kp.ShortID), kp.ShortID)
	}

	// Validate base64 decoding
	privBytes, err := base64.RawURLEncoding.DecodeString(kp.PrivateKey)
	if err != nil || len(privBytes) != 32 {
		t.Errorf("invalid private key bytes: %v, len: %d", err, len(privBytes))
	}

	pubBytes, err := base64.RawURLEncoding.DecodeString(kp.PublicKey)
	if err != nil || len(pubBytes) != 32 {
		t.Errorf("invalid public key bytes: %v, len: %d", err, len(pubBytes))
	}

	// Validate hex decoding
	shortBytes, err := hex.DecodeString(kp.ShortID)
	if err != nil || len(shortBytes) != 8 {
		t.Errorf("invalid short ID bytes: %v, len: %d", err, len(shortBytes))
	}
}
