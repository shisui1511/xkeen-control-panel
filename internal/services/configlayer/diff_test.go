package configlayer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8(t *testing.T) {
	short, trunc := truncateUTF8([]byte("abc"), 10)
	if short != "abc" || trunc {
		t.Errorf("короткая строка: %q, %v", short, trunc)
	}
	exact, trunc := truncateUTF8([]byte("abcd"), 4)
	if exact != "abcd" || trunc {
		t.Errorf("ровно по границе: %q, %v", exact, trunc)
	}
	cut, trunc := truncateUTF8([]byte("abcdef"), 4)
	if cut != "abcd" || !trunc {
		t.Errorf("обрезка ASCII: %q, %v", cut, trunc)
	}
	// «я» занимает 2 байта: граница 5 попадает в середину символа.
	ru := strings.Repeat("я", 10)
	cut, trunc = truncateUTF8([]byte(ru), 5)
	if !trunc || len(cut) > 5 || !utf8.ValidString(cut) || cut != "яя" {
		t.Errorf("обрезка по символу: %q (%d байт), truncated %v", cut, len(cut), trunc)
	}
}
