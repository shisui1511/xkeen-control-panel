//go:build router

package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// rtForeignAPIMaxSize — размер, с которого конфиг не просматривается на ссылки
// (geodata.foreignMaxSize = 16 МиБ): файл на один байт больше.
const rtForeignAPIMaxSize = 16<<20 + 1

// rtForeignAPIPlace — имя нечитаемого профиля Mihomo в каталоге profiles.
const rtForeignAPIPlace = "profiles/rt-unreadable.yaml"

// rtForeignAPIHasPlace — есть ли среди мест foreign ответа impact нечитаемый профиль.
func rtForeignAPIHasPlace(places []any) bool {
	for _, p := range places {
		m, _ := p.(map[string]any)
		line, hasLine := m["line"].(float64)
		if m != nil && m["file"] == rtForeignAPIPlace && hasLine && line == 0 {
			return true
		}
	}
	return false
}

// TestRouterGeodataForeignUnreadableAPI — на настоящей панели нечитаемый профиль
// Mihomo (разреженный файл больше 16 МиБ; неактивный профиль ядро не читает) виден в
// impact местом со строкой 0, а пометка удаления отвечает 409 foreign_ref с тем же
// местом в data.refs; ревизия черновика не меняется. После уборки файла место
// пропадает.
func TestRouterGeodataForeignUnreadableAPI(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()

	// Лежащий источник без пометки.
	file, mode := "", ""
	rows, _ := e.rtGeoSnapshot()["sources"].([]any)
	for _, row := range rows {
		m, _ := row.(map[string]any)
		name, _ := m["file"].(string)
		if m == nil || name == "" || m["present"] != true || m["draft"] != nil {
			continue
		}
		file, mode = name, "delete"
		if m["origin"] == "standard" {
			mode = "off"
		}
		break
	}
	if file == "" {
		t.Skip("в снимке нет лежащего источника без пометки")
	}

	// Профили Mihomo: каталог создаётся, если его нет, и убирается только созданный.
	profiles := filepath.Join(rtRtMihomoDir, "profiles")
	if _, err := os.Stat(profiles); os.IsNotExist(err) {
		if err := os.MkdirAll(profiles, 0o755); err != nil {
			t.Fatalf("создание каталога профилей: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(profiles) })
	}
	path := filepath.Join(rtRtMihomoDir, filepath.FromSlash(rtForeignAPIPlace))
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("создание нечитаемого профиля: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if err := f.Truncate(rtForeignAPIMaxSize); err != nil {
		_ = f.Close()
		t.Fatalf("Truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	imp := e.call(http.MethodGet, rtGeoBase+"/impact?file="+file, nil)
	if imp.Status != http.StatusOK {
		t.Fatalf("impact: %d %s", imp.Status, imp.Code)
	}
	foreign, _ := imp.object(t)["foreign"].([]any)
	if !rtForeignAPIHasPlace(foreign) {
		t.Errorf("impact: нечитаемого профиля нет среди мест (мест: %d)", len(foreign))
	}

	before := e.revision()
	r := e.call(http.MethodPost, rtGeoBase+"/drop", map[string]any{"rev": before, "file": file, "mode": mode, "actions": []any{}})
	if r.Status != http.StatusConflict || r.Code != "foreign_ref" {
		t.Fatalf("drop при нечитаемом профиле: статус %d, код %q; ожидалось 409 foreign_ref", r.Status, r.Code)
	}
	refs, _ := rtGeoData(t, r)["refs"].([]any)
	if !rtForeignAPIHasPlace(refs) {
		t.Errorf("drop: нечитаемого профиля нет в data.refs (мест: %d)", len(refs))
	}
	if got := e.revision(); got != before {
		t.Errorf("ревизия черновика %d, ожидалась прежняя %d", got, before)
	}

	// Файл убран: место пропало.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	imp = e.call(http.MethodGet, rtGeoBase+"/impact?file="+file, nil)
	if imp.Status != http.StatusOK {
		t.Fatalf("impact после уборки: %d %s", imp.Status, imp.Code)
	}
	if foreign, _ = imp.object(t)["foreign"].([]any); rtForeignAPIHasPlace(foreign) {
		t.Error("impact: место нечитаемого профиля осталось после уборки файла")
	}
}
