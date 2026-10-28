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

	t.Run("add", func(t *testing.T) {
		g, path := rtEditStore(t, tg.WorkDir, twoEntries, true)
		op := GeodataOp{Kind: OpAdd, File: "d.dat", URL: "https://example.invalid/d.dat"}
		p, err := g.Preview(op)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if !p.CommentsKept || p.Base != ContentBase(twoEntries) {
			t.Errorf("Preview: commentsKept=%v base_ok=%v", p.CommentsKept, p.Base == ContentBase(twoEntries))
		}
		for _, keep := range []string{"// комментарий сверху", "// обновлять", "/* про b */", "// про c"} {
			if !strings.Contains(p.After, keep) {
				t.Errorf("after потерял %q:\n%s", keep, p.After)
			}
		}
		if err := g.Apply(op, p.Base, false); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got := rtEntries(t, rtReadFile(t, path))
		if len(got) != 4 || got[3].File != "d.dat" || got[3].URL != op.URL {
			t.Errorf("записи после add: %v", got)
		}
		if err := g.Apply(op, "", false); ErrorCode(err) != "already_present" {
			t.Errorf("повторный add: %v", err)
		}

		// Без записи файла и без jq вставка не делается.
		g2, path2 := rtEditStore(t, tg.WorkDir, "{\n}\n", false)
		if _, err := g2.Preview(op); ErrorCode(err) != "no_jq" {
			t.Errorf("Preview add без jq: %v", err)
		}
		if err := g2.Apply(op, "", true); ErrorCode(err) != "no_jq" {
			t.Errorf("Apply add без jq: %v", err)
		}
		if rtReadFile(t, path2) != "{\n}\n" {
			t.Error("файл изменён при отказе no_jq")
		}
		// geofile_update: false — вставка не делается.
		off := "{\n  \"xkeen\": {\"geofile_update\": false}\n}\n"
		g3, path3 := rtEditStore(t, tg.WorkDir, off, true)
		if err := g3.Apply(op, "", true); ErrorCode(err) != "geofile_update_off" {
			t.Errorf("Apply add при geofile_update=false: %v", err)
		}
		if rtReadFile(t, path3) != off {
			t.Error("файл изменён при отказе geofile_update_off")
		}

		// Последняя запись без концевого комментария: вставка остаётся текстовой.
		const plain = "{\n  \"xkeen\": {\n    \"xray\": {\n      \"geodata\": [\n        {\"file\": \"a.dat\", \"url\": \"u1\"}\n      ]\n    }\n  }\n}\n"
		g4, _ := rtEditStore(t, tg.WorkDir, plain, true)
		p4, err := g4.Preview(op)
		if err != nil || !p4.CommentsKept || !strings.Contains(p4.After, "{\"file\": \"a.dat\", \"url\": \"u1\"},\n") {
			t.Errorf("add после записи без комментария: commentsKept=%v %v\n%s", p4.CommentsKept, err, p4.After)
		}
	})

	t.Run("set_url", func(t *testing.T) {
		g, path := rtEditStore(t, tg.WorkDir, twoEntries, true)
		const nasty = "https://example.invalid/a\"b\\c.dat"
		op := GeodataOp{Kind: OpSetURL, File: "a.dat", URL: nasty}
		p, err := g.Preview(op)
		if err != nil || !p.CommentsKept {
			t.Fatalf("Preview: commentsKept=%v %v", p.CommentsKept, err)
		}
		if err := g.Apply(op, p.Base, false); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		disk := rtReadFile(t, path)
		got := rtEntries(t, disk)
		if len(got) != 3 || got[0].URL != nasty || got[1].URL != "https://example.invalid/b.dat" || got[2].URL != "https://example.invalid/c.dat" {
			t.Errorf("записи после set_url: %v", got)
		}
		for _, keep := range []string{"// про a", "/* про b */", "// про c"} {
			if !strings.Contains(disk, keep) {
				t.Errorf("потерян комментарий %q:\n%s", keep, disk)
			}
		}
		if err := g.Apply(GeodataOp{Kind: OpSetURL, File: "zzz.dat", URL: "u"}, "", false); ErrorCode(err) != "entry_not_found" {
			t.Errorf("set_url неизвестного файла: %v", err)
		}
		if err := g.Apply(GeodataOp{Kind: OpSetURL, File: "a.dat", MatchURL: "other", URL: "u"}, "", false); ErrorCode(err) != "entry_not_found" {
			t.Errorf("set_url с чужим MatchURL: %v", err)
		}

		// Запись без url получает его.
		const noURL = "{\n  \"xkeen\": {\"xray\": {\"geodata\": [\n    {\"file\": \"a.dat\"}\n  ]}}\n}\n"
		g2, path2 := rtEditStore(t, tg.WorkDir, noURL, true)
		if err := g2.Apply(GeodataOp{Kind: OpSetURL, File: "a.dat", URL: "u1"}, "", false); err != nil {
			t.Fatalf("set_url записи без url: %v", err)
		}
		if got := rtEntries(t, rtReadFile(t, path2)); len(got) != 1 || got[0].URL != "u1" {
			t.Errorf("записи после set_url без url: %v", got)
		}
	})

	t.Run("gh_proxy", func(t *testing.T) {
		ghOf := func(t *testing.T, content string) (string, int) {
			t.Helper()
			root, err := decodeJSONC(content)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			xk, _ := root["xkeen"].(map[string]any)
			v, _ := xk["gh_proxy"].(string)
			return v, strings.Count(content, "\"gh_proxy\"")
		}
		// Ключа нет, объект xkeen есть, jq не нужен.
		g, path := rtEditStore(t, tg.WorkDir, twoEntries, false)
		if err := g.Apply(GeodataOp{Kind: OpGhProxy, Value: "https://example.invalid/p/"}, "", false); err != nil {
			t.Fatalf("Apply без ключа: %v", err)
		}
		disk := rtReadFile(t, path)
		if v, n := ghOf(t, disk); v != "https://example.invalid/p/" || n != 1 || !strings.Contains(disk, "/* про b */") {
			t.Errorf("gh_proxy без ключа: значение %q, вхождений %d\n%s", v, n, disk)
		}
		// Ключ есть — значение заменяется.
		if err := g.Apply(GeodataOp{Kind: OpGhProxy, Value: "https://example.invalid/q/"}, "", false); err != nil {
			t.Fatalf("Apply с ключом: %v", err)
		}
		if v, n := ghOf(t, rtReadFile(t, path)); v != "https://example.invalid/q/" || n != 1 {
			t.Errorf("gh_proxy с ключом: значение %q, вхождений %d", v, n)
		}
		// Нет объекта xkeen и нет файла.
		for _, content := range []string{"{\n}\n", "", "// только комментарий\n"} {
			g2, path2 := rtEditStore(t, tg.WorkDir, content, false)
			if err := g2.Apply(GeodataOp{Kind: OpGhProxy, Value: "p"}, "", true); err != nil {
				t.Fatalf("Apply в %q: %v", content, err)
			}
			if v, _ := ghOf(t, rtReadFile(t, path2)); v != "p" {
				t.Errorf("gh_proxy в %q: значение %q", content, v)
			}
		}
	})

	t.Run("enable_update", func(t *testing.T) {
		const nested = "// комментарий\n{\n  \"xkeen\": {\n    \"xray\": {\"x\": {\"geofile_update\" : false}},\n    \"geofile_update\":false // выключено\n  }\n}\n"
		g, path := rtEditStore(t, tg.WorkDir, nested, false)
		p, err := g.Preview(GeodataOp{Kind: OpEnableUpdate})
		if err != nil || !p.CommentsKept {
			t.Fatalf("Preview: commentsKept=%v %v", p.CommentsKept, err)
		}
		if !strings.Contains(p.After, "\"geofile_update\": true") || strings.Contains(p.After, "false") || !strings.Contains(p.After, "// выключено") {
			t.Errorf("after включения обновления:\n%s", p.After)
		}
		if err := g.Apply(GeodataOp{Kind: OpEnableUpdate}, p.Base, false); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, update, err := parseGeodata(rtReadFile(t, path)); err != nil || !update {
			t.Errorf("geofile_update после включения: %v %v", update, err)
		}
		// Ключа нет — файл не меняется.
		g2, path2 := rtEditStore(t, tg.WorkDir, twoEntries, false)
		p2, err := g2.Preview(GeodataOp{Kind: OpEnableUpdate})
		if err != nil || p2.After != p2.Before {
			t.Errorf("включение без false: after совпадает с before=%v %v", p2.After == p2.Before, err)
		}
		if err := g2.Apply(GeodataOp{Kind: OpEnableUpdate}, "", false); err != nil || rtReadFile(t, path2) != twoEntries || rtBackups(path2) != 0 {
			t.Errorf("Apply включения без false: %v", err)
		}
	})

	t.Run("status", func(t *testing.T) {
		const content = "{\n  \"xkeen\": {\n    \"gh_proxy\": \"https://example.invalid/p/\",\n    \"xray\": {\"geodata\": [\n      {\"file\": \"a.dat\", \"url\": \"u1\"},\n      {\"file\": \"b.dat\", \"url\": \"\"},\n      {\"file\": \"\", \"url\": \"u3\"},\n      {\"file\": \"c.dat\"}\n    ]}\n  }\n}\n"
		g, _ := rtEditStore(t, tg.WorkDir, content, true)
		st, err := g.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.GhProxy != "https://example.invalid/p/" || len(st.Entries) != 1 || st.Entries[0].File != "a.dat" || len(st.Invalid) != 3 {
			t.Errorf("Status: gh_proxy=%q записей=%d некорректных=%d", st.GhProxy, len(st.Entries), len(st.Invalid))
		}
	})

	t.Run("xkeen_json_invalid", func(t *testing.T) {
		// Файл разбирается, но проверка xkeen.json (policy не массив) отвергает запись.
		const bad = "{\n  \"xkeen\": {\"policy\": \"x\"}\n}\n"
		g, path := rtEditStore(t, tg.WorkDir, bad, true)
		if err := g.Apply(GeodataOp{Kind: OpGhProxy, Value: "p"}, "", false); ErrorCode(err) != "xkeen_json_invalid" {
			t.Errorf("Apply при отказе проверки: %v", err)
		}
		if rtReadFile(t, path) != bad {
			t.Error("файл изменён при отказе xkeen_json_invalid")
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
