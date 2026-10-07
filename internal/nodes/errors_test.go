package nodes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestErrorCodes_MatchFrontendList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "sourceErrorCodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var front []string
	if err := json.Unmarshal(data, &front); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range front {
		if seen[c] {
			t.Errorf("дубликат в sourceErrorCodes.json: %s", c)
		}
		seen[c] = true
	}
	all := AllCodes()
	if len(all) == 0 {
		t.Fatal("AllCodes пуст")
	}
	if !sort.StringsAreSorted(all) {
		t.Error("AllCodes должен быть отсортирован")
	}
	got := map[string]bool{}
	for _, c := range all {
		if got[c] {
			t.Errorf("дубликат в AllCodes: %s", c)
		}
		got[c] = true
		if !seen[c] {
			t.Errorf("код %s есть в Go, но нет в sourceErrorCodes.json", c)
		}
	}
	for c := range seen {
		if !got[c] {
			t.Errorf("код %s есть в sourceErrorCodes.json, но нет в Go", c)
		}
	}
	// 37 кодов ошибок и 5 примечаний уровня B.
	if len(all) != 42 {
		t.Errorf("кодов %d, ожидалось 42", len(all))
	}
	if !sort.StringsAreSorted(front) {
		t.Error("sourceErrorCodes.json должен быть в алфавитном порядке")
	}
}

func TestSourceError_JSONKeys(t *testing.T) {
	full := SourceError{
		Code:       CodeTimeout,
		Params:     map[string]string{"a": "b"},
		HTTPStatus: 502,
		Line:       3,
		Fragment:   "x",
		Detail:     "d",
	}
	keys := func(v any) map[string]bool {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}
	got := keys(full)
	want := []string{"code", "params", "http_status", "line", "fragment", "detail"}
	if len(got) != len(want) {
		t.Fatalf("ключи = %v", got)
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("нет ключа %s", k)
		}
	}

	only := keys(NewError(CodeTimeout))
	if len(only) != 1 || !only["code"] {
		t.Errorf("NewError(timeout) дал ключи %v", only)
	}

	e := NewError(CodeUnknownScheme, "scheme", "foo")
	if e.Params["scheme"] != "foo" || e.Code != "unknown_scheme" {
		t.Errorf("NewError: %+v", e)
	}
	if msg := e.Error(); msg == "" {
		t.Error("Error() пустой")
	}
}
