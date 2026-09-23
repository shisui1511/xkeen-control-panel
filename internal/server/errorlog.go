package server

import (
	"bytes"
	"io"
	"log"
)

// benignTLSErrors are handshake failures produced by browsers that reject the
// panel's self-signed certificate or drop a speculative connection. They are
// expected on every page load and only wear the router flash.
var benignTLSErrors = [][]byte{
	[]byte("unknown certificate"),
	[]byte("bad certificate"),
	[]byte("certificate unknown"),
	[]byte("EOF"),
	[]byte("connection reset by peer"),
}

type tlsNoiseFilter struct {
	out io.Writer
}

func (f tlsNoiseFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("TLS handshake error")) {
		for _, benign := range benignTLSErrors {
			if bytes.Contains(p, benign) {
				return len(p), nil
			}
		}
	}
	return f.out.Write(p)
}

// newServerErrorLog returns the logger used as http.Server.ErrorLog.
func newServerErrorLog() *log.Logger {
	return log.New(tlsNoiseFilter{out: log.Writer()}, log.Prefix(), log.Flags())
}
