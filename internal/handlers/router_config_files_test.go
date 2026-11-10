//go:build router

// router-area: configlayer

package handlers

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Файловый API вкладки «Файлы» на настоящей панели устройства (CFG-03): объём
// списка по scope=files (D-04), владелец у каждого файла (D-05), файлы с именем не
// латиницей в корне каталога Xray (видны, читаются, удаляются и переименовываются,
// но не сохраняются). Вердикт — коды и тела ответов панели и файлы на диске
// устройства. Ядро не читается и не переключается; файлы с такими именами XKeen
// может отказаться загружать, поэтому тест удаляет их до конца; флаг слоя
// возвращается прежним, черновик сбрасывается. Список последних применений файла
// панели (D-06) только читается: наборы копий тест не создаёт.

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

	rtFilesNonLatin(t, e)
	rtFilesApplies(t, e)
}

// rtFilesApplies — последние применения файла панели (D-06): только ключ
// манифеста и идентификатор набора копий, не путь.
func rtFilesApplies(t *testing.T, e *rtSrc) {
	t.Helper()
	const base = "/api/configlayer/files/applies?"
	// Файл pending ещё не записан и в манифест не входит.
	var files []rtLayerFile
	for _, f := range e.rtLayerFiles() {
		if f.State != "pending" {
			files = append(files, f)
		}
	}
	for _, f := range files {
		r := e.call(http.MethodGet, base+url.Values{"key": {f.Key}}.Encode(), nil)
		if r.Status != http.StatusOK {
			t.Errorf("применения %s: статус %d %s, ожидался 200", f.Key, r.Status, r.Code)
			continue
		}
		var view struct {
			Key     string `json:"key"`
			Path    string `json:"path"`
			Applies []struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
				Size   int64  `json:"size"`
			} `json:"applies"`
		}
		if err := json.Unmarshal(r.Data, &view); err != nil {
			t.Fatalf("применения %s: %v", f.Key, err)
		}
		// У отпущенного файла снимок пути не несёт.
		if view.Key != f.Key || (f.Path != "" && view.Path != f.Path) {
			t.Errorf("применения %s: key %q, path %q; ожидались ключ и путь из снимка", f.Key, view.Key, view.Path)
		}
		if len(view.Applies) > 5 {
			t.Errorf("применения %s: %d записей, ожидалось не больше 5", f.Key, len(view.Applies))
		}
		if len(view.Applies) == 0 {
			continue
		}
		first := view.Applies[0]
		q := url.Values{"key": {f.Key}, "id": {first.ID}}.Encode()
		c := e.call(http.MethodGet, base+q, nil)
		if c.Status != http.StatusOK {
			t.Errorf("версия до применения %s %s: статус %d %s", f.Key, first.ID, c.Status, c.Code)
			continue
		}
		var content struct {
			ID        string `json:"id"`
			Content   string `json:"content"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal(c.Data, &content); err != nil {
			t.Fatalf("версия до применения %s: %v", f.Key, err)
		}
		if content.ID != first.ID || (!content.Truncated && int64(len(content.Content)) != first.Size) {
			t.Errorf("версия до применения %s %s: id %q, длина %d при размере %d", f.Key, first.ID, content.ID, len(content.Content), first.Size)
		}
	}
	if len(files) == 0 {
		t.Logf("манифест пуст: список применений существующего ключа не проверен")
	}

	if r := e.call(http.MethodGet, base+url.Values{"key": {"xray:rt-absent.json"}}.Encode(), nil); r.Status != http.StatusNotFound {
		t.Errorf("применения ключа вне манифеста: статус %d, ожидался 404", r.Status)
	}
	// Путь вместо ключа не принимается.
	if r := e.call(http.MethodGet, base+url.Values{"key": {rtFilesXrayDir + "/01_log.json"}}.Encode(), nil); r.Status != http.StatusNotFound {
		t.Errorf("применения по пути вместо ключа: статус %d, ожидался 404", r.Status)
	}
	if len(files) == 0 {
		return
	}
	key := files[0].Key
	for _, bad := range []string{"../apply-1", "apply-1/../../state", "apply-1/x", "apply-x", "state"} {
		q := url.Values{"key": {key}, "id": {bad}}.Encode()
		if r := e.call(http.MethodGet, base+q, nil); r.Status != http.StatusBadRequest {
			t.Errorf("версия до применения с id %q: статус %d, ожидался 400", bad, r.Status)
		}
	}
	if r := e.call(http.MethodGet, base+url.Values{"key": {key}, "id": {"apply-1"}}.Encode(), nil); r.Status != http.StatusNotFound {
		t.Errorf("версия до несуществующего применения: статус %d, ожидался 404", r.Status)
	}
}

// rtFilesNonLatin — узкий путь для имён не латиницей в корне каталога Xray.
func rtFilesNonLatin(t *testing.T, e *rtSrc) {
	t.Helper()
	const (
		name    = "rt-файл (проверка).json"
		other   = "rt-удаление.json"
		renamed = "rt-files-renamed.json"
		content = `{}`
	)
	abs := filepath.Join(rtFilesXrayDir, name)
	otherAbs := filepath.Join(rtFilesXrayDir, other)
	renamedAbs := filepath.Join(rtFilesXrayDir, renamed)
	for _, p := range []string{abs, otherAbs, renamedAbs} {
		if _, err := os.Lstat(p); err == nil {
			t.Fatalf("файл %s уже есть на устройстве: тест его не трогает", filepath.Base(p))
		}
	}
	t.Cleanup(func() {
		for _, p := range []string{abs, otherAbs, renamedAbs} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("уборка %s: %v", filepath.Base(p), err)
			}
		}
	})
	for _, p := range []string{abs, otherAbs} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("запись %s: %v", filepath.Base(p), err)
		}
	}

	st, items := e.rtFilesList(rtFilesXrayDir, true)
	if st != http.StatusOK {
		t.Fatalf("список %s со scope=files: статус %d", rtFilesXrayDir, st)
	}
	found := map[string]rtFileItem{}
	for _, it := range items {
		found[it.Name] = it
	}
	for _, n := range []string{name, other} {
		it, ok := found[n]
		switch {
		case !ok:
			t.Errorf("файл с именем не латиницей %q не виден в списке scope=files", n)
		case !it.Restricted:
			t.Errorf("файл %q: restricted=false, ожидался true", n)
		case it.Owner != "user":
			t.Errorf("файл %q: owner %q, ожидался user", n, it.Owner)
		}
	}
	// Скобки в имени — слово стоп-списка XKeen «()».
	if it := found[name]; it.StoplistWord != "()" {
		t.Errorf("файл %q: stoplist_word %q, ожидалось «()»", name, it.StoplistWord)
	}
	// Без scope список прежний: такие файлы не показываются.
	if _, plain := e.rtFilesList(rtFilesXrayDir, false); plain != nil {
		for _, it := range plain {
			if it.Name == name || it.Name == other {
				t.Errorf("список без scope показал файл с именем не латиницей %q", it.Name)
			}
		}
	}

	pathQ := func(p string) string { return url.Values{"path": {p}}.Encode() }
	if r := e.call(http.MethodGet, "/api/config/read?"+pathQ(abs), nil); r.Status != http.StatusOK || string(r.Raw) != content {
		t.Errorf("чтение %q: статус %d, содержимое %q; ожидалось 200 и %q", name, r.Status, r.Raw, content)
	}
	// Сохранение и создание по имени не латиницей не расширены.
	if r := e.call(http.MethodPost, "/api/config/save?"+pathQ(abs), map[string]bool{"rt": true}); r.Status != http.StatusForbidden {
		t.Errorf("сохранение %q: статус %d, ожидался 403", name, r.Status)
	}
	created := filepath.Join(rtFilesXrayDir, "rt-создание.json")
	t.Cleanup(func() { _ = os.Remove(created) })
	if r := e.call(http.MethodPost, "/api/config/create?"+pathQ(created), nil); r.Status != http.StatusForbidden {
		t.Errorf("создание файла с именем не латиницей: статус %d, ожидался 403", r.Status)
	}
	// Обход узкой проверки: «..», подкаталог, обратная косая черта и NUL.
	for _, bad := range []string{
		rtFilesXrayDir + "/../файл.json",
		rtFilesXrayDir + "/подкаталог/файл.json",
		rtFilesXrayDir + "/a\\файл.json",
		rtFilesXrayDir + "/файл\x00.json",
		"/opt/etc/xcp/файл.json",
	} {
		if r := e.call(http.MethodGet, "/api/config/read?"+pathQ(bad), nil); r.Status != http.StatusForbidden {
			t.Errorf("чтение недопустимого пути %q: статус %d, ожидался 403", bad, r.Status)
		}
		if r := e.call(http.MethodPost, "/api/config/delete?"+pathQ(bad), nil); r.Status != http.StatusForbidden {
			t.Errorf("удаление недопустимого пути %q: статус %d, ожидался 403", bad, r.Status)
		}
	}

	// Переименование в латинское имя — путь исправления.
	q := url.Values{"old": {abs}, "new": {renamedAbs}}.Encode()
	if r := e.call(http.MethodPost, "/api/config/rename?"+q, nil); r.Status != http.StatusOK {
		t.Errorf("переименование %q в %q: статус %d %s", name, renamed, r.Status, r.Error)
	} else {
		if _, err := os.Stat(renamedAbs); err != nil {
			t.Errorf("после переименования нет %s: %v", renamed, err)
		}
		if _, err := os.Lstat(abs); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("после переименования прежнее имя %q осталось на диске", name)
		}
	}
	// Новое имя не латиницей не принимается.
	q = url.Values{"old": {otherAbs}, "new": {filepath.Join(rtFilesXrayDir, "rt-новое.json")}}.Encode()
	if r := e.call(http.MethodPost, "/api/config/rename?"+q, nil); r.Status != http.StatusForbidden {
		t.Errorf("переименование в имя не латиницей: статус %d, ожидался 403", r.Status)
	}
	// Удаление файла с именем не латиницей.
	if r := e.call(http.MethodPost, "/api/config/delete?"+pathQ(otherAbs), nil); r.Status != http.StatusOK {
		t.Errorf("удаление %q: статус %d %s", other, r.Status, r.Error)
	} else if _, err := os.Lstat(otherAbs); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("после удаления файл %q остался на диске", other)
	}
}
