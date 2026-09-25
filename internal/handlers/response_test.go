package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJSONError_NoDoubleEscaping: вывод валидатора доходит до UI как есть,
// без HTML-сущностей, а сырой JSON не содержит опасных символов.
func TestJSONError_NoDoubleEscaping(t *testing.T) {
	msg := `failed to parse "outbounds": <nil> & more`
	rr := httptest.NewRecorder()
	JSONError(rr, http.StatusBadRequest, msg)

	if raw := rr.Body.String(); strings.ContainsAny(raw, "<>") {
		t.Errorf("raw JSON must not contain < or >: %s", raw)
	}
	var resp APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != msg {
		t.Errorf("error = %q, want %q", resp.Error, msg)
	}
}
