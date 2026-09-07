package services

import (
	"testing"
)

func TestSubscriptionNodeClone(t *testing.T) {
	jc := 4
	jmin := 40
	jmax := 70
	s1 := 15
	s2 := 40
	s3 := 20
	s4 := 30
	cpa := 10
	rt := true
	dc := false
	rekey := 120

	orig := SubscriptionNode{
		Tag:            "test-node",
		Name:           "Test Node",
		Protocol:       "wireguard",
		Server:         "1.2.3.4:51820",
		SecretKey:      "privKeyBase64=",
		PublicKey:      "pubKeyBase64=",
		PreSharedKey:   "pskBase64=",
		Reserved:       []int{1, 2, 3},
		MTU:            1280,
		LocalAddresses: []string{"10.0.0.2/32"},
		AllowedIPs:     []string{"0.0.0.0/0"},
		KeepAlive:      25,
		AWG: &AWGOptions{
			Jc:                     &jc,
			Jmin:                   &jmin,
			Jmax:                   &jmax,
			S1:                     &s1,
			S2:                     &s2,
			S3:                     &s3,
			S4:                     &s4,
			H1:                     "1000000001",
			H2:                     "1000000002",
			H3:                     "1000000003",
			H4:                     "1000000004",
			Version:                "3.1",
			HeaderProtectionKey:    "test-secret-key",
			I1:                     "0A1B2C",
			I2:                     "3D4E5F",
			I3:                     "112233",
			I4:                     "445566",
			I5:                     "778899",
			ContentPaddingAddition: &cpa,
			RandomTrailers:         &rt,
			DisableCookies:         &dc,
			RekeyAfterTime:         &rekey,
			RawOptions: map[string]interface{}{
				"custom_future_opt": "val123",
			},
		},
	}

	cloned := orig.Clone()

	// Проверяем идентичность полей
	if cloned.Tag != orig.Tag || cloned.Protocol != orig.Protocol {
		t.Fatalf("mismatched basic fields: got tag=%s, proto=%s", cloned.Tag, cloned.Protocol)
	}

	if cloned.AWG == nil {
		t.Fatalf("expected cloned.AWG to not be nil")
	}

	if *cloned.AWG.Jc != jc || cloned.AWG.HeaderProtectionKey != "test-secret-key" {
		t.Fatalf("mismatched AWG fields in clone")
	}

	// Проверяем глубокое копирование (мутация клона не аффектит оригинал)
	*cloned.AWG.Jc = 99
	cloned.AWG.H1 = "9999999999"
	cloned.Reserved[0] = 999
	cloned.LocalAddresses[0] = "192.168.1.1/32"
	cloned.AWG.RawOptions["custom_future_opt"] = "mutated"

	if *orig.AWG.Jc != jc {
		t.Errorf("original Jc mutated: expected %d, got %d", jc, *orig.AWG.Jc)
	}
	if orig.AWG.H1 != "1000000001" {
		t.Errorf("original H1 mutated: expected 1000000001, got %s", orig.AWG.H1)
	}
	if orig.Reserved[0] != 1 {
		t.Errorf("original Reserved mutated: expected 1, got %d", orig.Reserved[0])
	}
	if orig.LocalAddresses[0] != "10.0.0.2/32" {
		t.Errorf("original LocalAddresses mutated: expected 10.0.0.2/32, got %s", orig.LocalAddresses[0])
	}
	if orig.AWG.RawOptions["custom_future_opt"] != "val123" {
		t.Errorf("original RawOptions mutated: expected val123, got %v", orig.AWG.RawOptions["custom_future_opt"])
	}
}
