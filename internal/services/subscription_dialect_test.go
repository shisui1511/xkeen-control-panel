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

	// 4. 1.5
	j1 := 50
	node15 := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			Jc: &jc,
			J1: &j1,
		},
	}
	if d := DetectWireGuardDialect(node15); d != Dialect15 {
		t.Errorf("expected 1.5, got %s", d)
	}

	// 5. 3.1 (HeaderProtectionKey, I1, v3 timing)
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

	rekeyTimeout := 60
	node31Timing := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			RekeyTimeout: &rekeyTimeout,
		},
	}
	if d := DetectWireGuardDialect(node31Timing); d != Dialect31 {
		t.Errorf("expected 3.1 for RekeyTimeout, got %s", d)
	}

	// 6. Test InferAWGVersion
	nodeToInfer31 := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			HeaderProtectionKey: "secret",
		},
	}
	if ver := InferAWGVersion(nodeToInfer31); ver != "3.1" || nodeToInfer31.AWG.Version != "3.1" {
		t.Errorf("expected auto-inferred 3.1 version, got %s (AWG.Version: %s)", ver, nodeToInfer31.AWG.Version)
	}

	nodeToInfer15 := &SubscriptionNode{
		Protocol: "wireguard",
		AWG: &AWGOptions{
			J1: &j1,
		},
	}
	if ver := InferAWGVersion(nodeToInfer15); ver != "1.5" || nodeToInfer15.AWG.Version != "1.5" {
		t.Errorf("expected auto-inferred 1.5 version, got %s", ver)
	}
}
