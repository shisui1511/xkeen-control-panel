package services

import (
	"testing"
)

func TestDetectWireGuardDialect(t *testing.T) {
	// 1. Plain
	nodePlain := &SubscriptionNode{
		Protocol: "wireguard",
	}
	if d := DetectWireGuardDialect(nodePlain); d != DialectPlain {
		t.Errorf("expected plain, got %s", d)
	}

	nodeEmptyAWG := &SubscriptionNode{
		Protocol: "wireguard",
		AWG:      &AWGOptions{},
	}
	if d := DetectWireGuardDialect(nodeEmptyAWG); d != DialectPlain {
		t.Errorf("expected plain for empty AWG, got %s", d)
	}

	// 2. Classic
	jc := 4
	nodeClassic := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			Jc: &jc,
			H1: "1000000001",
		},
	}
	if d := DetectWireGuardDialect(nodeClassic); d != DialectClassic {
		t.Errorf("expected classic, got %s", d)
	}

	// 3. 2.0
	s3 := 20
	node20 := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			Jc: &jc,
			S3: &s3,
		},
	}
	if d := DetectWireGuardDialect(node20); d != Dialect20 {
		t.Errorf("expected 2.0, got %s", d)
	}

	// 4. 3.1
	node31 := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			Jc:                  &jc,
			S3:                  &s3,
			HeaderProtectionKey: "secret-key",
		},
	}
	if d := DetectWireGuardDialect(node31); d != Dialect31 {
		t.Errorf("expected 3.1, got %s", d)
	}

	node31Hex := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			I1: "0A1B2C",
		},
	}
	if d := DetectWireGuardDialect(node31Hex); d != Dialect31 {
		t.Errorf("expected 3.1 for I1, got %s", d)
	}
}
