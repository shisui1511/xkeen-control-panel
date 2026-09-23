package server

import (
	"bytes"
	"testing"
)

func TestTLSNoiseFilter(t *testing.T) {
	var buf bytes.Buffer
	f := tlsNoiseFilter{out: &buf}

	noise := []string{
		"http: TLS handshake error from 172.16.0.136:18322: remote error: tls: unknown certificate\n",
		"http: TLS handshake error from 172.16.0.136:1234: EOF\n",
	}
	for _, line := range noise {
		if n, err := f.Write([]byte(line)); err != nil || n != len(line) {
			t.Fatalf("unexpected write result %d %v", n, err)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("benign TLS noise was logged: %q", buf.String())
	}

	kept := "http: TLS handshake error from 1.2.3.4:5: tls: client offered only unsupported versions\n"
	_, _ = f.Write([]byte(kept))
	other := "http: panic serving 1.2.3.4:5: boom\n"
	_, _ = f.Write([]byte(other))
	if buf.String() != kept+other {
		t.Fatalf("expected meaningful errors to pass through, got %q", buf.String())
	}
}
