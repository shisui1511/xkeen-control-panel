//go:build router

package handlers

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Выключение перехвата при ручной правке файла панели, не связанного с перехватом
// (WR-13). Трасса: подготовка → включение и «Оставить» → в файл панели, который
// слушателя :53 не содержит, дописан перевод строки (дрейф: хэш на диске не
// совпал с манифестом) → «Выключить» → state off без сбоя, порт 53 у прошивки,
// прошивка отвечает через 127.0.0.1:53, слушателя в файлах ядра нет, правка
// пользователя на месте (панель её не перезаписала, D-11). t.Cleanup: исходное
// содержимое файла возвращено до очистки стенда. Только Xray: у Mihomo слушатель
// лежит в единственном файле панели, не затронутых перехватом файлов нет. Правится
// файл, содержимое которого не меняется от включения (хэши до и после), иначе перехват
// законно переписал бы его сам. Метка ядра: цель привязана к активному ядру.
func TestRouterDNSDisableDrift(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	if kernel != configlayer.KernelXray {
		t.Skip("у Mihomo слушатель :53 лежит в единственном файле панели: файла без перехвата для правки нет")
	}
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	// Хэши файлов панели до включения: правке подходит только файл, который включение
	// не меняет, — а значит, не меняет и выключение (его перепишет только перехват).
	before := rtDNSPanelHashes(e)
	e.rtDNSEnableOK()

	var victim string
	for p, h := range rtDNSPanelHashes(e) {
		if filepath.Base(p) == rtDNSXrayListenerFile {
			continue
		}
		if prev, ok := before[p]; ok && prev == h && (victim == "" || p < victim) {
			victim = p
		}
	}
	if victim == "" {
		t.Skip("в снимке слоя нет файла панели, не затронутого перехватом: править нечего")
	}
	orig, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("файл панели %s не читается: %v", filepath.Base(victim), err)
	}
	edited := append(append([]byte(nil), orig...), '\n')
	t.Cleanup(func() {
		if err := os.WriteFile(victim, orig, 0o644); err != nil {
			t.Errorf("очистка: файл панели %s не возвращён: %v", filepath.Base(victim), err)
		}
	})
	if err := os.WriteFile(victim, edited, 0o644); err != nil {
		t.Fatalf("правка файла панели %s: %v", filepath.Base(victim), err)
	}

	e.rtDNSStart(rtDNSDisable)
	v := e.rtDNSWaitDone()
	if st, _ := v["state"].(string); st != "off" {
		code, detail := rtDNSFailure(v)
		t.Fatalf("выключение при дрейфе вне перехвата: state %q, ожидалось off: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после выключения порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после выключения прошивка не отвечает через 127.0.0.1:53")
	}
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после выключения в файлах %s остался слушатель :53", kernel)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("файл панели %s не читается после выключения: %v", filepath.Base(victim), err)
	}
	if string(got) != string(edited) {
		t.Errorf("панель перезаписала правку пользователя в %s", filepath.Base(victim))
	}
}

// rtDNSPanelHashes — хэши содержимого json-файлов панели из снимка слоя, которые есть на
// диске сейчас (файл, появляющийся или исчезающий с перехватом, виден только с одной
// стороны сравнения).
func rtDNSPanelHashes(e *rtSrc) map[string][sha256.Size]byte {
	e.t.Helper()
	out := map[string][sha256.Size]byte{}
	for _, p := range e.rtDNSPanelFiles() {
		if filepath.Ext(p) != ".json" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out[p] = sha256.Sum256(data)
	}
	return out
}
