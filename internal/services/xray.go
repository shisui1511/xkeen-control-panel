package services

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// RealityKeypair holds generated Reality credentials.
type RealityKeypair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
}

// GenerateRealityKeypair generates a new x25519 keypair and random 8-byte short ID for Xray Reality.
func GenerateRealityKeypair() (*RealityKeypair, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	pub := priv.PublicKey()

	// Xray Reality uses RawURLEncoding (base64 without padding)
	privKeyStr := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubKeyStr := base64.RawURLEncoding.EncodeToString(pub.Bytes())

	shortBytes := make([]byte, 8)
	if _, err := rand.Read(shortBytes); err != nil {
		return nil, fmt.Errorf("failed to generate short id: %w", err)
	}
	shortIDStr := hex.EncodeToString(shortBytes)

	return &RealityKeypair{
		PrivateKey: privKeyStr,
		PublicKey:  pubKeyStr,
		ShortID:    shortIDStr,
	}, nil
}
