//go:build router

// router-area: configlayer

package handlers

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/legacyarchive"
)

// HTTP-контракт окна старого слоя на настоящей панели устройства (CUT-03,
// D-02/D-06). Старые данные готовятся при остановленной панели, архив создаёт
// её штатный старт. Адреса подписок в сценарии синтетические (домен .invalid),
// генерируются при запуске и в сообщения, вложения и журнал теста не попадают.

const (
	rtLegacyPath      = "/api/configlayer/legacy"
	rtLegacyClosePath = rtLegacyPath + "/close"
	rtLegacyCleanPath = rtLegacyPath + "/clean"
	// rtLegacyForgetWait — срок уборки списка адресов: закрытие убирает его
	// сразу, а при занятом замке очистки — следующая сверка слоя (раз в минуту).
	rtLegacyForgetWait = 150 * time.Second
	// rtLegacyStreamWait — предел ожидания событий SSE и скачивания диагностики.
	rtLegacyStreamWait = 90 * time.Second
)

// rtLegacyView — ответ GET legacy (data).
type rtLegacyView struct {
	Pending       bool     `json:"pending"`
	ArchivePath   string   `json:"archive_path"`
	URLsAvailable bool     `json:"urls_available"`
	URLs          []string `json:"urls"`
}

// rtLegacyURLs — синтетические адреса старых подписок сценария.
func rtLegacyURLs() []string {
	urls := make([]string, 2)
	for i := range urls {
		urls[i] = "https://rt-legacy-" + strings.ToLower(rand.Text()[:10]) + ".invalid/sub/" + strings.ToLower(rand.Text())
	}
	return urls
}

// rtLegacySeed при остановленной панели убирает прежний архив и признак закрытия
// окна в файле состояния слоя и кладёт реестр старых подписок с адресами urls.
// Остальные поля файла состояния сохраняются байт в байт.
func rtLegacySeed(t *testing.T, urls []string) {
	t.Helper()
	data := config.Default().DataDir
	if err := os.RemoveAll(filepath.Join(data, legacyarchive.Directory)); err != nil {
		t.Fatalf("прежний архив старых данных не удалён: %v", err)
	}
	statePath := filepath.Join(data, configlayer.StateFileName)
	if raw, err := os.ReadFile(statePath); err == nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("файл состояния слоя не разбирается: %v", err)
		}
		if _, closed := fields["legacy"]; closed {
			delete(fields, "legacy")
			out, err := json.MarshalIndent(fields, "", "  ")
			if err != nil || os.WriteFile(statePath, out, 0o600) != nil {
				t.Fatal("признак закрытия окна не снят в файле состояния")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("файл состояния слоя не читается: %v", err)
	}
	records := make([]map[string]string, len(urls))
	for i, u := range urls {
		records[i] = map[string]string{"id": "rt-legacy-" + string(rune('a'+i)), "url": u}
	}
	raw, err := json.Marshal(records)
	if err != nil || os.WriteFile(filepath.Join(data, "subscriptions.json"), raw, 0o600) != nil {
		t.Fatal("реестр старых подписок не записан")
	}
}

// rtLegacyGet читает окно с проверкой Cache-Control: no-store. Тело с адресами
// в сообщения не попадает.
func rtLegacyGet(t *testing.T, p *routertest.Panel) rtLegacyView {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), rtLegacyStreamWait)
	defer cancel()
	resp, err := p.Stream(ctx, rtLegacyPath)
	if err != nil {
		t.Fatalf("GET %s: сетевая ошибка", rtLegacyPath)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: статус %d, ожидался 200", rtLegacyPath, resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET %s: Cache-Control %q, ожидался no-store", rtLegacyPath, cc)
	}
	var body struct {
		Data rtLegacyView `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		t.Fatalf("GET %s: ответ не разобран", rtLegacyPath)
	}
	return body.Data
}

// rtLegacyContains сообщает, есть ли в data хотя бы один адрес из urls.
func rtLegacyContains(data []byte, urls []string) bool {
	return slices.ContainsFunc(urls, func(u string) bool { return bytes.Contains(data, []byte(u)) })
}

// rtLegacyEvents читает поток событий слоя и отдаёт пары «событие, данные».
func rtLegacyEvents(t *testing.T, p *routertest.Panel) (<-chan [2]string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*rtLegacyStreamWait)
	resp, err := p.Stream(ctx, "/api/configlayer/events")
	if err != nil || resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatal("поток событий слоя не открылся")
	}
	out := make(chan [2]string, 64)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				select {
				case out <- [2]string{name, strings.TrimPrefix(line, "data: ")}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, func() { cancel(); wg.Wait() }
}

// rtLegacyNextEvent ждёт событие name и возвращает его данные.
func rtLegacyNextEvent(t *testing.T, events <-chan [2]string, name string) string {
	t.Helper()
	deadline := time.After(rtLegacyStreamWait)
	for {
		select {
		case ev, open := <-events:
			if !open {
				t.Fatalf("поток событий закрылся до события %s", name)
			}
			if ev[0] == name {
				return ev[1]
			}
		case <-deadline:
			t.Fatalf("событие %s не пришло за %v", name, rtLegacyStreamWait)
		}
	}
}

// rtLegacyDiagnosticsLeak скачивает диагностику и ищет адреса во всех записях.
func rtLegacyDiagnosticsLeak(t *testing.T, p *routertest.Panel, urls []string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), rtLegacyStreamWait)
	defer cancel()
	resp, err := p.Stream(ctx, "/api/system/diagnostics")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal("диагностика не скачана")
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal("диагностика не архив gzip")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {
			t.Fatal("архив диагностики не читается")
		}
		if rtLegacyContains([]byte(hdr.Name), urls) {
			return true
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal("запись диагностики не читается")
		}
		if rtLegacyContains(content, urls) {
			return true
		}
	}
}

// Трасса: контракт доступа (401 без сессии, 403 без CSRF, 405 чужим методом,
// 400 с телом и параметрами) → остановка панели, реестр старых подписок с
// синтетическими адресами → штатный старт архивирует его → GET legacy: окно
// ждёт закрытия, адреса совпадают, no-store → снимок, первое событие SSE и
// диагностика без адресов → закрытие одновременно двумя клиентами → событие
// legacy только со счётчиком, второй клиент видит закрытое окно без адресов,
// повтор безопасен, список адресов убран с диска → перезапуск панели: закрытие
// сохранилось. Ядро не читается и не перезапускается: очистка следов в каталогах
// ядер проверяется TestRouterLegacyArchive на файлах устройства.
func TestRouterLegacyAPI(t *testing.T) {
	routertest.Slow(t, "два перезапуска панели init-скриптом: однократный архив на старте и сохранность закрытия окна")
	e := rtNewSrc(t)
	second := routertest.NewPanel(t)

	t.Run("access", func(t *testing.T) {
		checks := []struct {
			method, path string
			body         []byte
			auth         routertest.PanelAuth
			want         int
		}{
			{http.MethodGet, rtLegacyPath, nil, routertest.AuthNone, http.StatusUnauthorized},
			{http.MethodPost, rtLegacyClosePath, nil, routertest.AuthNone, http.StatusUnauthorized},
			{http.MethodPost, rtLegacyCleanPath, nil, routertest.AuthNone, http.StatusUnauthorized},
			{http.MethodPost, rtLegacyClosePath, nil, routertest.AuthNoCSRF, http.StatusForbidden},
			{http.MethodPost, rtLegacyCleanPath, nil, routertest.AuthNoCSRF, http.StatusForbidden},
			{http.MethodGet, rtLegacyClosePath, nil, routertest.AuthFull, http.StatusMethodNotAllowed},
			{http.MethodGet, rtLegacyCleanPath, nil, routertest.AuthFull, http.StatusMethodNotAllowed},
			{http.MethodPost, rtLegacyPath, nil, routertest.AuthFull, http.StatusMethodNotAllowed},
			{http.MethodPost, rtLegacyCleanPath, []byte(`{"path":"/opt/etc/xray/configs"}`), routertest.AuthFull, http.StatusBadRequest},
			{http.MethodPost, rtLegacyCleanPath + "?path=05_routing.json", nil, routertest.AuthFull, http.StatusBadRequest},
			{http.MethodPost, rtLegacyClosePath, []byte(`{"closed":true}`), routertest.AuthFull, http.StatusBadRequest},
		}
		for _, c := range checks {
			status, _, err := e.p.Request(c.method, c.path, c.body, c.auth)
			if err != nil {
				t.Fatalf("%s %s: сетевая ошибка", c.method, c.path)
			}
			if status != c.want {
				t.Errorf("%s %s: статус %d, ожидался %d", c.method, c.path, status, c.want)
			}
		}
	})

	urls := rtLegacyURLs()
	old := rtPanelCurrent(t, e.p)
	rtDNSPanelStop(t, old)
	rtLegacySeed(t, urls)
	rtDNSPanelStart(t, e, old)
	dataDir := config.Default().DataDir
	if _, err := os.Stat(filepath.Join(dataDir, "subscriptions.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("старт панели не убрал реестр старых подписок в архив")
	}

	view := rtLegacyGet(t, e.p)
	if !view.Pending || !view.URLsAvailable || view.ArchivePath == "" {
		t.Fatalf("окно после архива: pending=%v urls_available=%v путь архива задан=%v", view.Pending, view.URLsAvailable, view.ArchivePath != "")
	}
	got := slices.Clone(view.URLs)
	want := slices.Clone(urls)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("адресов в окне %d, ожидалось %d (совпадение набора нарушено)", len(view.URLs), len(urls))
	}

	t.Run("no-leak", func(t *testing.T) {
		r := e.call(http.MethodGet, "/api/configlayer/state", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
		}
		if rtLegacyContains(r.Raw, urls) {
			t.Error("снимок слоя содержит адрес старой подписки")
		}
		legacy, _ := r.object(t)["legacy"].(map[string]any)
		if pending, _ := legacy["pending"].(bool); !pending {
			t.Error("снимок слоя не показывает ожидающее окно")
		}
		if _, ok := legacy["urls"]; ok {
			t.Error("снимок слоя несёт поле urls")
		}
		events, stop := rtLegacyEvents(t, e.p)
		snapshot := rtLegacyNextEvent(t, events, configlayer.EventSnapshot)
		stop()
		if rtLegacyContains([]byte(snapshot), urls) {
			t.Error("первое событие SSE содержит адрес старой подписки")
		}
		if rtLegacyDiagnosticsLeak(t, e.p, urls) {
			t.Error("диагностика содержит адрес старой подписки")
		}
	})

	events, stopEvents := rtLegacyEvents(t, second)
	defer stopEvents()
	rtLegacyNextEvent(t, events, configlayer.EventSnapshot)

	// Две вкладки закрывают окно одновременно: обе получают один исход.
	type closeResult struct {
		status int
		view   rtLegacyView
		err    error
	}
	results := make(chan closeResult, 2)
	for _, p := range []*routertest.Panel{e.p, second} {
		go func() {
			var body rtLegacyView
			status, err := p.PostJSON(rtLegacyClosePath, nil, &struct {
				Data *rtLegacyView `json:"data"`
			}{&body})
			results <- closeResult{status, body, err}
		}()
	}
	for range 2 {
		r := <-results
		if r.err != nil || r.status != http.StatusOK {
			t.Fatalf("закрытие окна: статус %d, ожидался 200", r.status)
		}
		if r.view.Pending || r.view.URLsAvailable || r.view.ArchivePath != view.ArchivePath {
			t.Errorf("закрытие окна: pending=%v urls_available=%v путь архива прежний=%v", r.view.Pending, r.view.URLsAvailable, r.view.ArchivePath == view.ArchivePath)
		}
	}
	data := rtLegacyNextEvent(t, events, configlayer.EventLegacy)
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &payload) != nil || len(payload) != 1 || payload["seq"] == nil {
		t.Error("событие legacy несёт что-то кроме счётчика")
	}
	if rtLegacyContains([]byte(data), urls) {
		t.Error("событие legacy содержит адрес старой подписки")
	}

	for _, p := range []*routertest.Panel{second, e.p} {
		if v := rtLegacyGet(t, p); v.Pending || v.URLsAvailable || len(v.URLs) != 0 || v.ArchivePath != view.ArchivePath {
			t.Errorf("окно после закрытия: pending=%v адресов %d путь архива прежний=%v", v.Pending, len(v.URLs), v.ArchivePath == view.ArchivePath)
		}
	}
	if status, err := second.PostJSON(rtLegacyClosePath, nil, nil); err != nil || status != http.StatusOK {
		t.Errorf("повторное закрытие: статус %d, ожидался 200", status)
	}
	urlsFile := filepath.Join(dataDir, legacyarchive.Directory, "urls.json")
	routertest.WaitCond(t, rtLegacyForgetWait, "список адресов убран после закрытия", func() bool {
		_, err := os.Lstat(urlsFile)
		return errors.Is(err, os.ErrNotExist)
	})
	if _, err := os.Lstat(filepath.Join(dataDir, legacyarchive.Directory, "old-panel.tar.gz")); err != nil {
		t.Error("архив старых данных пропал после закрытия окна")
	}

	t.Run("restart", func(t *testing.T) {
		old := rtPanelCurrent(t, e.p)
		res := rtPanelRestart(t, e.p, old, nil)
		rtPanelRestartVerdict(t, old, res, "перезапуск после закрытия окна старого слоя")
		if v := rtLegacyGet(t, e.p); v.Pending || len(v.URLs) != 0 || v.ArchivePath != view.ArchivePath {
			t.Errorf("после перезапуска: pending=%v адресов %d путь архива прежний=%v", v.Pending, len(v.URLs), v.ArchivePath == view.ArchivePath)
		}
	})
}
