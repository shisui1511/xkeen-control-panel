//go:build router

// router-area: configlayer

package legacyarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestRouterLegacyArchive(t *testing.T) {
	workDir := routertest.Current(t).WorkDir
	t.Run("clean", func(t *testing.T) {
		data := newArchiveTestDir(t, workDir)
		writeArchiveTestFile(t, data, "cache/rulesets/current", []byte("current"))
		for _, empty := range []string{"subscriptions", "cache/providers"} {
			if err := os.MkdirAll(filepath.Join(data, empty), 0700); err != nil {
				t.Fatal("не удалось подготовить пустой каталог")
			}
		}
		result, err := Run(data)
		if err != nil || result != (Result{}) {
			t.Fatal("чистая установка вернула результат архивации")
		}
		requireMissing(t, filepath.Join(data, Directory))
	})

	t.Run("first-start", func(t *testing.T) {
		data := newArchiveTestDir(t, workDir)
		writeArchiveTestFile(t, data, "user_rules.json", []byte("[]"))
		results := make(chan Result, 2)
		for range 2 {
			go func() {
				result, _ := Run(data)
				results <- result
			}()
		}
		first, second := <-results, <-results
		if !first.Complete || first.Warning != "" || second != first {
			t.Fatal("старты не получили один завершённый архив")
		}
		requireMode(t, first.ArchivePath, 0600)
		requireMissing(t, filepath.Join(data, "user_rules.json"))
	})

	t.Run("archive-and-resume", func(t *testing.T) {
		data := newArchiveTestDir(t, workDir)
		// Неразрешимый контролируемый адрес из сценариев источников. Сеть
		// в этом сценарии не нужна, адрес проверяется только в памяти теста.
		address := (&url.URL{Scheme: "https", Host: "xcp-provider.invalid", Path: "/sub"}).String()
		registry, err := json.Marshal([]map[string]any{{
			"id": "test-sub", "provider_name": "test_provider", "url": address,
			"nodes": []map[string]string{{"url": "ignored-node-field"}},
		}})
		if err != nil {
			t.Fatal("не удалось записать реестр для сценария")
		}
		oldFiles := map[string][]byte{
			"subscriptions/subscriptions.json": registry,
			"subscriptions/test_raw.txt":       []byte("old nodes"),
			"subscriptions.json":               registry,
			"cache/providers/test.yaml":        []byte("proxies: []"),
			"user_rules.json":                  []byte("[]"),
		}
		protected := map[string][]byte{
			"data/xcp_hwid.txt":           []byte("012345abcdef"),
			configlayer.StateFileName:     []byte(`{"schema_version":1,"applied":{},"draft":{},"live":{},"wizard":{},"manifest":{}}`),
			"config_layer/state":          []byte("layer"),
			"backup/mihomo/config.yaml":   []byte("backup"),
			"sessions/session.json":       []byte("{}"),
			"cache/rulesets/current.yaml": []byte("payload: []"),
			"kernels/current":             []byte("kernel"),
			"geodata/current":             []byte("geodata"),
			"smartproxy/current":          []byte("smartproxy"),
			"traffic/current":             []byte("traffic"),
		}
		for path, content := range oldFiles {
			writeArchiveTestFile(t, data, path, content)
		}
		for path, content := range protected {
			writeArchiveTestFile(t, data, path, content)
		}
		legacyPath := filepath.Join(data, Directory)
		if err := os.Mkdir(legacyPath, 0700); err != nil {
			t.Fatal("не удалось создать каталог сценария архивации")
		}
		d := openArchiveTestRoot(t, data)
		l := openArchiveTestRoot(t, legacyPath)
		preparation, err := prepare(d, l)
		if err != nil || preparation == nil || preparation.State != prepared {
			t.Fatal("не удалось подготовить архив")
		}
		for path, content := range oldFiles {
			requireFileBytes(t, filepath.Join(data, path), content)
		}
		archivePath := filepath.Join(legacyPath, archiveName)
		archiveInfo := requireMode(t, archivePath, 0600)
		requireMode(t, legacyPath, 0700)
		requireMode(t, filepath.Join(legacyPath, markerName), 0600)
		requireMode(t, filepath.Join(legacyPath, urlsName), 0600)
		if preparation.URLCount != 1 || len(preparation.Ownership) != 2 ||
			preparation.Ownership[0] != (ownership{ID: "test-sub", ProviderName: "test_provider"}) {
			t.Fatal("неверные сведения об адресах или владельцах")
		}
		f, err := os.Open(archivePath)
		if err != nil {
			t.Fatal("не удалось открыть подготовленный архив")
		}
		files := tarDigests(t, f)
		f.Close()
		if len(files) != len(oldFiles) {
			t.Fatal("архив содержит неожиданный набор файлов")
		}
		for name, content := range oldFiles {
			if files[name] != sha256.Sum256(content) {
				t.Fatal("содержимое архивного файла отличается от источника")
			}
		}
		markerBytes, err := os.ReadFile(filepath.Join(legacyPath, markerName))
		if err != nil || bytes.Contains(markerBytes, []byte(address)) {
			t.Fatal("секрет вышел за границу закрытых файлов")
		}
		urlBytes, err := os.ReadFile(filepath.Join(legacyPath, urlsName))
		var addresses []string
		if err != nil || json.Unmarshal(urlBytes, &addresses) != nil ||
			len(addresses) != 1 || addresses[0] != address {
			t.Fatal("отдельный файл адресов не совпал с подпиской")
		}

		// Удаляем одну реальную запись штатным шагом и оставляем prepared
		// на диске. Run продолжает с этого места по сохранённому маркеру.
		if err := d.removeArchived(preparation.Sources[:1], ""); err != nil {
			t.Fatal("не удалось остановить удаление после первого источника")
		}
		requireMissing(t, filepath.Join(data, preparation.Sources[0].Name))
		result, err := Run(data)
		if err != nil || !result.Complete || result.ArchivePath != archivePath {
			t.Fatal("повторный старт не завершил подготовленный архив")
		}
		requireMissing(t, filepath.Join(data, "subscriptions"))
		requireMissing(t, filepath.Join(data, "cache/providers"))
		for path := range oldFiles {
			requireMissing(t, filepath.Join(data, path))
		}
		for path, content := range protected {
			requireFileBytes(t, filepath.Join(data, path), content)
		}
		assertArchiveExcluded(t, data, legacyPath, urlBytes)

		// Список адресов может быть убран после закрытия окна следующим планом.
		if err := l.Root.Remove(urlsName); err != nil {
			t.Fatal("не удалось убрать файл адресов перед повторным запуском")
		}
		repeated, err := Run(data)
		if err != nil || repeated != result || !os.SameFile(archiveInfo, requireMode(t, archivePath, 0600)) {
			t.Fatal("повторный запуск изменил завершённый архив")
		}
		requireMissing(t, filepath.Join(legacyPath, urlsName))
		archives, err := filepath.Glob(filepath.Join(legacyPath, "*.tar.gz"))
		if err != nil || len(archives) != 1 {
			t.Fatal("ожидался один архив после повторных запусков")
		}
	})

	t.Run("write-failure", func(t *testing.T) {
		data := newArchiveTestDir(t, workDir)
		original := []byte("[]")
		writeArchiveTestFile(t, data, "user_rules.json", original)
		if err := os.MkdirAll(filepath.Join(data, Directory, urlsName+".tmp"), 0700); err != nil {
			t.Fatal("не удалось подготовить конфликт записи")
		}
		result, err := Run(data)
		if err == nil || result.Complete || result.Warning == "" {
			t.Fatal("ожидался отказ записи с безопасной причиной")
		}
		requireFileBytes(t, filepath.Join(data, "user_rules.json"), original)
		requireMissing(t, filepath.Join(data, Directory, markerName))
	})

	t.Run("external-symlinks", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			target func(dir, file string) string
		}{
			{"subscriptions", func(_, file string) string { return file }},
			{"cache/providers", func(dir, _ string) string { return dir }},
			{Directory, func(dir, _ string) string { return dir }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				data := newArchiveTestDir(t, workDir)
				external := newArchiveTestDir(t, workDir)
				safe := []byte("external checkpoint")
				file := writeArchiveTestFile(t, external, "external.json", safe)
				before := requireMode(t, file, 0600)
				original := []byte("[]")
				writeArchiveTestFile(t, data, "user_rules.json", original)
				link := filepath.Join(data, tc.name)
				if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
					t.Fatal("не удалось создать родительский каталог ссылки")
				}
				target := tc.target(external, file)
				if err := os.Symlink(target, link); err != nil {
					t.Fatal("не удалось создать контрольную ссылку")
				}
				result, err := Run(data)
				if err == nil || result.Complete || result.Warning == "" {
					t.Fatal("ожидался отказ работы через ссылку")
				}
				requireFileBytes(t, filepath.Join(data, "user_rules.json"), original)
				requireFileBytes(t, file, safe)
				requireMode(t, external, 0700)
				if !os.SameFile(before, requireMode(t, file, 0600)) {
					t.Fatal("изменилась внешняя цель")
				}
				if current, err := os.Readlink(link); err != nil || current != target {
					t.Fatal("изменилась ссылка")
				}
				requireMissing(t, filepath.Join(data, Directory, archiveName))
				requireMissing(t, filepath.Join(external, archiveName))
			})
		}
	})
}

func assertArchiveExcluded(t *testing.T, data, legacy string, urlBytes []byte) {
	t.Helper()
	files := services.NewSnapshotService(data, []string{data, legacy})
	control := []byte("control")
	writeArchiveTestFile(t, data, "control.json", control)
	snapshot, err := files.Create("проверка исключений")
	if err != nil {
		t.Fatal("не удалось создать снимок")
	}
	path, err := files.ArchivePath(snapshot.ID)
	if err != nil {
		t.Fatal("не удалось найти снимок")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("не удалось открыть снимок")
	}
	digests := tarDigests(t, f)
	f.Close()
	if digests[filepath.Base(data)+"/control.json"] != sha256.Sum256(control) {
		t.Fatal("снимок пропустил контрольный файл")
	}
	assertNoLegacyEntries(t, digests)

	alias := filepath.Join(data, "legacy-alias.json")
	if err := os.Symlink(filepath.Join(legacy, urlsName), alias); err != nil {
		t.Fatal("не удалось создать алиас для проверки исключений")
	}
	cfg := config.Default()
	cfg.DataDir = data
	cfg.XRayConfigDir = data
	cfg.MihomoConfigDir = legacy
	cfg.ConfigPath = alias
	cfg.LogSources = []string{alias}
	cfg.LogPath = alias
	cfg.AllowedRoots = []string{data}
	response := httptest.NewRecorder()
	handlers.NewAPI(cfg, nil).DiagnosticsDownload(response, httptest.NewRequest(http.MethodGet, "/api/system/diagnostics", nil))
	if response.Code != http.StatusOK {
		t.Fatal("диагностика не собрана")
	}
	diagnostic := tarDigests(t, response.Body)
	if _, ok := diagnostic["configs/xray/control.json"]; !ok {
		t.Fatal("диагностика пропустила контрольный файл")
	}
	assertNoLegacyEntries(t, diagnostic)
	for _, forbidden := range []string{"configs/xcp-config.json", "logs/legacy-alias.json"} {
		if _, ok := diagnostic[forbidden]; ok {
			t.Fatal("диагностика прочитала архив через алиас")
		}
	}

	var payload bytes.Buffer
	gz := gzip.NewWriter(&payload)
	tw := tar.NewWriter(gz)
	restored := []byte("restored")
	for name, content := range map[string][]byte{
		"control.json":             restored,
		Directory + "/" + urlsName: []byte("unexpected overwrite"),
		filepath.Base(alias):       []byte("unexpected overwrite through alias"),
	} {
		header := &tar.Header{Name: filepath.Base(data) + "/" + name, Mode: 0600, Size: int64(len(content))}
		if tw.WriteHeader(header) != nil {
			t.Fatal("не удалось подготовить заголовок снимка")
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal("не удалось подготовить содержимое снимка")
		}
	}
	if errors.Join(tw.Close(), gz.Close()) != nil {
		t.Fatal("не удалось закрыть снимок")
	}
	upload, err := files.SaveUploaded(&payload, "restore.tar.gz")
	if err != nil {
		t.Fatal("не удалось загрузить снимок")
	}
	if _, err := files.Restore(upload.ID); err != nil {
		t.Fatal("не удалось восстановить снимок")
	}
	requireFileBytes(t, filepath.Join(data, "control.json"), restored)
	requireFileBytes(t, filepath.Join(legacy, urlsName), urlBytes)
}

func assertNoLegacyEntries(t *testing.T, entries map[string][sha256.Size]byte) {
	t.Helper()
	for name := range entries {
		if strings.Contains("/"+name+"/", "/"+Directory+"/") {
			t.Fatal("приватный каталог попал в экспорт")
		}
		switch filepath.Base(name) {
		case archiveName, markerName, urlsName, "legacy-alias.json":
			t.Fatal("приватный файл попал в экспорт")
		}
	}
}

func tarDigests(t *testing.T, reader io.Reader) map[string][sha256.Size]byte {
	t.Helper()
	gz, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatal("ожидался архив gzip")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := make(map[string][sha256.Size]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal("не удалось прочитать архив tar")
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		digest := sha256.New()
		if _, err := io.Copy(digest, tr); err != nil {
			t.Fatal("не удалось прочитать запись архива")
		}
		files[hdr.Name] = [sha256.Size]byte(digest.Sum(nil))
	}
}

func newArchiveTestDir(t *testing.T, parent string) string {
	t.Helper()
	dir, err := os.MkdirTemp(parent, "legacy-archive-")
	if err != nil {
		t.Fatal("не удалось создать рабочий каталог сценария")
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeArchiveTestFile(t *testing.T, dir, rel string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, content, 0600) != nil {
		t.Fatal("не удалось создать файл сценария")
	}
	return path
}

func openArchiveTestRoot(t *testing.T, path string) archiveDir {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal("не удалось закрепить рабочий каталог сценария")
	}
	t.Cleanup(func() { _ = root.Close() })
	info, err := root.Stat(".")
	if err != nil {
		t.Fatal("не удалось получить сведения о рабочем каталоге")
	}
	return archiveDir{Root: root, validator: utils.NewPathValidator([]string{path}), info: info}
}

func requireFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(content) != sha256.Sum256(expected) {
		t.Fatal("контрольный файл исчез или изменился")
	}
}

func requireMode(t *testing.T, path string, mode os.FileMode) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != mode {
		t.Fatal("файл или каталог отсутствует либо имеет неверные права")
	}
	return info
}

func requireMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("ожидалось отсутствие файла")
	}
}
