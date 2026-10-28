//go:build router

package geodata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Сценарии WR-05 на устройстве: чужой конфиг, который не удалось прочитать, не
// превращается в «ссылок нет». Разреженный файл больше foreignMaxSize и FIFO вместо
// профиля — настоящие случаи «не прочитан» (root игнорирует права доступа, chmod не
// годится); разреженный файл не пишет данные на флешку. Ядро не переключается —
// метки ядра нет.

// rtForeignSparse создаёт разреженный файл размером foreignMaxSize+1.
func rtForeignSparse(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(foreignMaxSize + 1); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
}

// rtForeignHas — есть ли среди мест {file, line}.
func rtForeignHas(refs []ForeignRef, file string, line int) bool {
	for _, r := range refs {
		if r.File == file && r.Line == line {
			return true
		}
	}
	return false
}

// TestRouterGeodataForeignUnreadable — нечитаемый чужой конфиг (слишком большой файл,
// не обычный файл) даёт место со строкой 0, и удаление блокируется на двух путях:
// пометка (Drop) и шаг после «Применить» (AfterApply); файл dat остаётся на месте.
func TestRouterGeodataForeignUnreadable(t *testing.T) {
	e := rtRemoveNewEnv(t)
	e.apply(t)

	// (1) Слишком большой конфиг Xray вне манифеста.
	big := filepath.Join(e.xrayDir, "08_big.json")
	rtForeignSparse(t, big)
	refs, err := e.geo.ForeignRefs(rtRemoveFile)
	if err != nil {
		t.Fatalf("ForeignRefs: %v", err)
	}
	if !rtForeignHas(refs, "08_big.json", 0) {
		t.Errorf("нечитаемый конфиг не блокирует удаление: %+v", refs)
	}
	before := e.rev(t)
	_, err = e.geo.Drop(before, rtRemoveFile, routing.DropDelete, nil)
	var fe *ForeignRefError
	if !errors.As(err, &fe) || !errors.Is(err, ErrForeignRef) {
		t.Errorf("Drop при нечитаемом конфиге: %v, ожидалась foreign_ref", err)
	}
	if got := e.rev(t); got != before {
		t.Errorf("ревизия черновика %d, ожидалась прежняя %d", got, before)
	}
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
	if refs, _ = e.geo.ForeignRefs(rtRemoveFile); len(refs) != 0 {
		t.Errorf("после уборки файла остались места: %+v", refs)
	}

	// (2) FIFO вместо профиля Mihomo: не обычный файл.
	fifo := filepath.Join(e.mihomoDir, "profiles", "fifo.yaml")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}
	refs, err = e.geo.ForeignRefs(rtRemoveFile)
	if err != nil {
		t.Fatalf("ForeignRefs: %v", err)
	}
	if !rtForeignHas(refs, "profiles/fifo.yaml", 0) {
		t.Errorf("FIFO вместо профиля не блокирует удаление: %+v", refs)
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}

	// (3) Шаг после «Применить»: пометка в применённом состоянии, а конфиг не прочитан.
	rtForeignSparse(t, big)
	if err := e.layer.SettleSection(routing.GeodataSection, func(_, draft json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		applied, err := json.Marshal(routing.GeodataDraft{Drop: []routing.GeodataDrop{{File: rtRemoveFile, Mode: routing.DropDelete}}})
		return applied, draft, err
	}); err != nil {
		t.Fatalf("SettleSection: %v", err)
	}
	e.geo.AfterApply(context.Background(), configlayer.AfterApplyInfo{})
	if !e.exists(rtRemoveFile) {
		t.Error("файл удалён при нечитаемом чужом конфиге")
	}
	found := false
	for _, p := range e.pending() {
		if p.File == rtRemoveFile && p.Reason == ErrForeignRef.Code {
			found = true
		}
	}
	if !found {
		t.Errorf("в незавершённых удалениях нет %s с причиной %s: %+v", rtRemoveFile, ErrForeignRef.Code, e.pending())
	}
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
}
