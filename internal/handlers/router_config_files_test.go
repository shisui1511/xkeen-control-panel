//go:build router

// router-area: configlayer

package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

// Файловый API вкладки «Файлы» на настоящей панели устройства (CFG-03): объём
// списка по scope=files (D-04), владелец у каждого файла (D-05). Вердикт — коды и
// тела ответов панели. Ядро не читается и не переключается; флаг слоя
// возвращается прежним, черновик сбрасывается.

const rtFilesXrayDir = "/opt/etc/xray/configs"

// rtFileItem — элемент ответа GET /api/config/list.
type rtFileItem struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Owner        string   `json:"owner"`
	TakeKernel   string   `json:"take_kernel"`
	TakeSections []string `json:"take_sections"`
	StoplistWord string   `json:"stoplist_word"`
	Restricted   bool     `json:"restricted"`
}

// rtFilesList запрашивает список каталога и возвращает статус и элементы (при 200).
func (e *rtSrc) rtFilesList(dir string, scoped bool) (int, []rtFileItem) {
	e.t.Helper()
	q := url.Values{"dir": {dir}}
	if scoped {
		q.Set("scope", "files")
	}
	r := e.call(http.MethodGet, "/api/config/list?"+q.Encode(), nil)
	if r.Status != http.StatusOK {
		return r.Status, nil
	}
	var items []rtFileItem
	if err := json.Unmarshal(r.Raw, &items); err != nil {
		e.t.Fatalf("список %s: ответ не массив: %v", dir, err)
	}
	return r.Status, items
}

// rtLayerFile — файл манифеста из снимка слоя.
type rtLayerFile struct {
	Key    string `json:"key"`
	Kernel string `json:"kernel"`
	Path   string `json:"path"`
	State  string `json:"state"`
}

// rtLayerFiles читает файлы манифеста из снимка слоя.
func (e *rtSrc) rtLayerFiles() []rtLayerFile {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	var snap struct {
		Files []rtLayerFile `json:"files"`
	}
	if err := json.Unmarshal(r.Data, &snap); err != nil {
		e.t.Fatalf("снимок слоя: %v", err)
	}
	return snap.Files
}

func TestRouterConfigFilesScope(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	// Фоновая сборка включения пишет файлы панели: владелец сверяется после неё.
	e.rtRstIdle()

	// Объём «Файлов» (D-04): данные панели вне трёх каталогов — отказ.
	if st, _ := e.rtFilesList("/opt/etc/xcp", true); st != http.StatusForbidden {
		t.Errorf("список /opt/etc/xcp со scope=files: статус %d, ожидался 403", st)
	}
	// Без scope поведение прежнее.
	if st, _ := e.rtFilesList("/opt/etc/xcp", false); st != http.StatusOK {
		t.Errorf("список /opt/etc/xcp без scope: статус %d, ожидался 200", st)
	}

	st, items := e.rtFilesList(rtFilesXrayDir, true)
	if st != http.StatusOK {
		t.Fatalf("список %s со scope=files: статус %d, ожидался 200", rtFilesXrayDir, st)
	}
	byName := map[string]rtFileItem{}
	for _, it := range items {
		byName[it.Name] = it
		switch it.Owner {
		case "panel", "xkeen", "user":
		default:
			t.Errorf("файл %s: owner %q вне panel, xkeen, user", it.Name, it.Owner)
		}
		if it.Owner == "panel" && (it.TakeKernel != "" || len(it.TakeSections) > 0) {
			t.Errorf("файл панели %s: предложено взять секции %s %v", it.Name, it.TakeKernel, it.TakeSections)
		}
		if it.TakeKernel != "" && it.TakeKernel != "xray" {
			t.Errorf("файл %s каталога Xray: take_kernel %q, ожидался xray", it.Name, it.TakeKernel)
		}
	}
	// Без scope новых полей нет: ответ прежний.
	if _, plain := e.rtFilesList(rtFilesXrayDir, false); len(plain) > 0 {
		for _, it := range plain {
			if it.Owner != "" || it.TakeKernel != "" || it.StoplistWord != "" || it.Restricted {
				t.Errorf("список без scope: у файла %s появились поля вкладки «Файлы»", it.Name)
			}
		}
	}
	// Файл манифеста в каталоге Xray — владелец panel.
	panelSeen := 0
	for _, f := range e.rtLayerFiles() {
		if f.Kernel != "xray" || f.State == "released" || filepath.Dir(f.Path) != rtFilesXrayDir {
			continue
		}
		it, ok := byName[filepath.Base(f.Path)]
		if !ok {
			continue // файл пропал с диска (дрейф) — в списке его нет
		}
		panelSeen++
		if it.Owner != "panel" {
			t.Errorf("файл манифеста %s: owner %q, ожидался panel", it.Name, it.Owner)
		}
	}
	if panelSeen == 0 {
		t.Logf("в каталоге Xray нет файлов манифеста: проверка владельца panel пропущена")
	}
}
