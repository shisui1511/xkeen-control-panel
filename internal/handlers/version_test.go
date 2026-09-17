package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestVersionHandler(t *testing.T) {
	srv, err := server.New(&server.Config{Port: 8090}, "v0.15.0", fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}

	api := &API{
		xkeenSvc: services.NewXKeenService("/bin/true", t.TempDir()),
		srv:      srv,
	}

	// 1. Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	recPost := httptest.NewRecorder()
	api.Version(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. GET returns version info
	reqGet := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	recGet := httptest.NewRecorder()
	api.Version(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}
}
