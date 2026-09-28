package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type xkeenStoplistCase struct {
	Name string  `json:"name"`
	Word *string `json:"word"`
}

// TestXKeenStoplistMatch_SharedCases читает тот же фикстур, что и vitest:
// расхождение правила в Go и TypeScript роняет оба теста.
func TestXKeenStoplistMatch_SharedCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "xkeenStoplist.cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []xkeenStoplistCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 40 {
		t.Fatalf("фикстур пуст или мал: %d кейсов", len(cases))
	}
	for _, c := range cases {
		word, ok := XKeenStoplistMatch(c.Name)
		switch {
		case c.Word == nil && ok:
			t.Errorf("%q: совпадения быть не должно, получено слово %q", c.Name, word)
		case c.Word != nil && (!ok || word != *c.Word):
			t.Errorf("%q: ожидалось слово %q, получено %q (ok=%v)", c.Name, *c.Word, word, ok)
		}
	}
}

func TestXKeenStoplistMatch_Fold(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ABC", "abc"},
		{"MiXeD.JSON", "mixed.json"},
		{"КОПИЯ", "копия"},
		{"Копия", "копия"},
		{"ЁЖ", "ёж"},
		{"Ёлка", "ёлка"},
		{"ẞ", "ẞ"},
		{"日本Z", "日本z"},
		{"(1)-_.", "(1)-_."},
		{"", ""},
	}
	for _, c := range cases {
		if got := foldXKeen(c.in); got != c.want {
			t.Errorf("foldXKeen(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
