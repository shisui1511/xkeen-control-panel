package utils

import (
	"testing"
	"time"
)

func TestLocationFromPOSIX_FixedOffset(t *testing.T) {
	loc, err := LocationFromPOSIX("MSK-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC).In(loc)
	if ts.Hour() != 15 {
		t.Errorf("expected 15h local for MSK-3, got %d", ts.Hour())
	}
	if name, off := ts.Zone(); name != "MSK" || off != 3*3600 {
		t.Errorf("unexpected zone %s %d", name, off)
	}
}

func TestLocationFromPOSIX_DST(t *testing.T) {
	loc, err := LocationFromPOSIX("CET-1CEST,M3.5.0,M10.5.0/3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).In(loc)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).In(loc)
	if winter.Hour() != 13 {
		t.Errorf("winter: expected 13h, got %d", winter.Hour())
	}
	if summer.Hour() != 14 {
		t.Errorf("summer: expected 14h, got %d", summer.Hour())
	}
}

func TestLocationFromPOSIX_AngleBracketName(t *testing.T) {
	loc, err := LocationFromPOSIX("<+05>-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, off := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).In(loc).Zone(); off != 5*3600 {
		t.Errorf("expected +5h offset, got %d", off)
	}
}

func TestLocationFromPOSIX_Invalid(t *testing.T) {
	for _, tz := range []string{"", "Europe/Moscow", "MS-3", "MSK", "MSK -3", "garbage\n"} {
		if _, err := LocationFromPOSIX(tz); err == nil {
			t.Errorf("expected error for %q", tz)
		}
	}
}
