//go:build router

package lists

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Роутерный сценарий общего редактора xkeen.json (GeodataStore.Preview/Apply). Все
// правки идут на копии файла во временном каталоге рабочей папки теста: настоящий
// xkeen.json стенда и xkeen -ug тест не трогает. Ядро не читается — метки нет.

// rtEditStore создаёт хранилище на временном каталоге с заданным xkeen.json
// (единственный вызов NewGeodataStore в этом файле).
func rtEditStore(t *testing.T, workDir, content string, jq bool) (*GeodataStore, string) {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, "geodata-edit")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "xkeen.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := services.NewXKeenSettingsService(dir, dir, []string{dir})
	look := func(string) (string, error) { return "", exec.ErrNotFound }
	if jq {
		look = func(string) (string, error) { return "jq", nil }
	}
	return NewGeodataStore(svc, look, nil), path
}

func rtReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rtEntries возвращает записи geodata файла (корректные и некорректные).
func rtEntries(t *testing.T, content string) []GeodataEntry {
	t.Helper()
	root, err := decodeJSONC(content)
	if err != nil {
		t.Fatalf("разбор файла: %v", err)
	}
	_, arr, _ := geodataArray(root)
	var out []GeodataEntry
	for _, el := range arr {
		m, _ := el.(map[string]any)
		f, _ := m["file"].(string)
		u, _ := m["url"].(string)
		out = append(out, GeodataEntry{File: f, URL: u})
	}
	return out
}

func rtBackups(path string) int {
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backup", "xkeen", "xkeen.json.*"))
	return len(m)
}

func TestRouterGeodataEdit(t *testing.T) {
	tg := routertest.Current(t)

	const twoEntries = "// комментарий сверху\n" +
		"{\n" +
		"  \"xkeen\": {\n" +
		"    \"geofile_update\": true, // обновлять\n" +
		"    \"xray\": {\n" +
		"      \"geodata\": [\n" +
		"        {\"file\": \"a.dat\", \"url\": \"https://example.invalid/a.dat\"}, // про a\n" +
		"        /* про b */ {\"file\": \"b.dat\", \"url\": \"https://example.invalid/b.dat\"},\n" +
		"        {\"file\": \"c.dat\", \"url\": \"https://example.invalid/c.dat\"} // про c\n" +
		"      ]\n" +
		"    }\n" +
		"  }\n" +
		"}\n"

	t.Run("remove", func(t *testing.T) {
		g, path := rtEditStore(t, tg.WorkDir, twoEntries, true)

		p, err := g.Preview(GeodataOp{Kind: OpRemove, File: "a.dat"})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if !p.CommentsKept || p.Before != twoEntries || p.Base != ContentBase(twoEntries) {
			t.Fatalf("Preview: commentsKept=%v before_same=%v base_ok=%v", p.CommentsKept, p.Before == twoEntries, p.Base == ContentBase(twoEntries))
		}
		for _, keep := range []string{"// комментарий сверху", "// обновлять", "/* про b */", "// про c", "b.dat", "c.dat"} {
			if !strings.Contains(p.After, keep) {
				t.Errorf("after потерял %q:\n%s", keep, p.After)
			}
		}
		if strings.Contains(p.After, "a.dat") || strings.Contains(p.After, "// про a") {
			t.Errorf("after сохранил удалённую запись:\n%s", p.After)
		}
		if rtReadFile(t, path) != twoEntries {
			t.Fatal("Preview изменил файл")
		}

		// Запись по отпечатку чужого файла отменяется, файл остаётся как был.
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "a.dat"}, ContentBase(twoEntries+" "), false); ErrorCode(err) != "geodata_changed" {
			t.Errorf("Apply со старым base: %v", err)
		}
		if rtReadFile(t, path) != twoEntries {
			t.Fatal("файл изменён при отказе geodata_changed")
		}

		// Запись с другим адресом при MatchURL не удаляется.
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "a.dat", MatchURL: "https://example.invalid/other.dat"}, "", false); ErrorCode(err) != "entry_not_found" {
			t.Errorf("remove с чужим MatchURL: %v", err)
		}
		if rtReadFile(t, path) != twoEntries {
			t.Fatal("файл изменён при отказе entry_not_found")
		}

		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "a.dat", MatchURL: "https://example.invalid/a.dat"}, p.Base, false); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if got := rtReadFile(t, path); got != p.After {
			t.Errorf("файл после Apply не равен предпросмотру:\n%s", got)
		}
		if rtBackups(path) == 0 {
			t.Error("резервная копия xkeen.json не появилась")
		}
		want := []GeodataEntry{{File: "b.dat", URL: "https://example.invalid/b.dat"}, {File: "c.dat", URL: "https://example.invalid/c.dat"}}
		if got := rtEntries(t, rtReadFile(t, path)); !reflect.DeepEqual(got, want) {
			t.Errorf("записи после удаления: %v", got)
		}

		// Повторный Apply со старым base — файл уже другой.
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "b.dat"}, p.Base, false); ErrorCode(err) != "geodata_changed" {
			t.Errorf("повторный Apply со старым base: %v", err)
		}

		// Последняя запись массива: запятая у предыдущей уходит, комментарий остаётся.
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "c.dat"}, "", false); err != nil {
			t.Fatalf("Apply последней записи: %v", err)
		}
		disk := rtReadFile(t, path)
		if !strings.Contains(disk, "/* про b */") || strings.Contains(disk, "c.dat") {
			t.Errorf("удаление последней записи:\n%s", disk)
		}
		if got := rtEntries(t, disk); len(got) != 1 || got[0].File != "b.dat" {
			t.Errorf("записи после удаления последней: %v", got)
		}

		// Единственная запись: массив остаётся пустым, файл остаётся допустимым.
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "b.dat"}, "", false); err != nil {
			t.Fatalf("Apply единственной записи: %v", err)
		}
		disk = rtReadFile(t, path)
		if got := rtEntries(t, disk); len(got) != 0 || !strings.Contains(disk, "// комментарий сверху") {
			t.Errorf("удаление единственной записи: %d записей\n%s", len(got), disk)
		}
	})

	t.Run("remove inline", func(t *testing.T) {
		const inline = "{\"xkeen\": {\"xray\": {\"geodata\": [{\"file\": \"a.dat\", \"url\": \"u1\"}, {\"file\": \"b.dat\", \"url\": \"u2\"}]}}}\n"
		for _, name := range []string{"a.dat", "b.dat"} {
			g, path := rtEditStore(t, tg.WorkDir, inline, true)
			p, err := g.Preview(GeodataOp{Kind: OpRemove, File: name})
			if err != nil || !p.CommentsKept {
				t.Fatalf("Preview %s: commentsKept=%v %v", name, p.CommentsKept, err)
			}
			if err := g.Apply(GeodataOp{Kind: OpRemove, File: name}, p.Base, false); err != nil {
				t.Fatalf("Apply %s: %v", name, err)
			}
			disk := rtReadFile(t, path)
			if strings.Contains(disk, "\""+name+"\"") || len(rtEntries(t, disk)) != 1 {
				t.Errorf("однострочный массив после удаления %s: %s", name, disk)
			}
		}
	})

	t.Run("remove all duplicates", func(t *testing.T) {
		const dup = "{\n  \"xkeen\": {\n    \"xray\": {\n      \"geodata\": [\n        {\"file\": \"a.dat\", \"url\": \"u1\"},\n        {\"file\": \"b.dat\", \"url\": \"u2\"},\n        {\"file\": \"a.dat\", \"url\": \"u3\"}\n      ]\n    }\n  }\n}\n"
		g, path := rtEditStore(t, tg.WorkDir, dup, true)
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "a.dat"}, "", false); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if got := rtEntries(t, rtReadFile(t, path)); len(got) != 1 || got[0].File != "b.dat" {
			t.Errorf("записи после удаления дублей: %v", got)
		}
	})

	t.Run("remove without geodata", func(t *testing.T) {
		g, path := rtEditStore(t, tg.WorkDir, "{\n}\n", true)
		if err := g.Apply(GeodataOp{Kind: OpRemove, File: "a.dat"}, "", false); ErrorCode(err) != "entry_not_found" {
			t.Errorf("remove в пустом файле: %v", err)
		}
		if rtReadFile(t, path) != "{\n}\n" {
			t.Error("файл изменён при отказе entry_not_found")
		}
	})
}
