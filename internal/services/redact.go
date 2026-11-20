package services

import (
	"bufio"
	"bytes"
	"io"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func StripANSI(s string) string { return utils.StripANSI(s) }

func StripCarriageReturns(s string) string { return utils.StripCarriageReturns(s) }

func NormalizeTimestamps(s string) string { return utils.NormalizeTimestamps(s) }

func RedactSensitiveText(s string) string { return utils.RedactSensitiveText(s) }

// RedactionReader wraps an io.Reader, sanitizing line by line on the fly.
type RedactionReader struct {
	scanner *bufio.Scanner
	buf     bytes.Buffer
}

// NewRedactionReader creates an io.Reader that streams redacted content.
func NewRedactionReader(r io.Reader) io.Reader {
	return &RedactionReader{
		scanner: bufio.NewScanner(r),
	}
}

func (rr *RedactionReader) Read(p []byte) (int, error) {
	if rr.buf.Len() == 0 {
		if !rr.scanner.Scan() {
			if err := rr.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		line := rr.scanner.Text()
		redacted := RedactSensitiveText(line)
		rr.buf.WriteString(redacted)
		rr.buf.WriteByte('\n')
	}
	return rr.buf.Read(p)
}
