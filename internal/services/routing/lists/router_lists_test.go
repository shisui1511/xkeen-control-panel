//go:build router

package lists

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Роутерные сценарии пакета lists: источник истины — настоящие dat-файлы XKeen
// на устройстве и вердикт ядра Xray (xray run -test), а не эталонные таблицы.

// rtDatIndex возвращает индекс каталога dat устройства; без файлов тест пропускается.
func rtDatIndex(t *testing.T) (*Index, []FileInfo) {
	t.Helper()
	ix := NewIndex(DefaultDatDir)
	files, err := ix.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) == 0 {
		t.Skipf("в %s нет dat-файлов: XKeen их не установил", DefaultDatDir)
	}
	return ix, files
}

func rtXrayBinary() string {
	if p, err := exec.LookPath("xray"); err == nil {
		return p
	}
	if _, err := os.Stat("/opt/sbin/xray"); err == nil {
		return "/opt/sbin/xray"
	}
	return ""
}

// rtXrayTest прогоняет `xray run -test` на конфиге с одним правилом ext:<файл>:<тег>
// и возвращает объединённый вывод и ошибку запуска.
func rtXrayTest(t *testing.T, bin, workDir, file, tag, kind string) (string, error) {
	t.Helper()
	field := "domain"
	if kind == KindIP {
		field = "ip"
	}
	cfg := map[string]any{
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
		"routing": map[string]any{
			"rules": []any{map[string]any{
				"type":        "field",
				field:         []string{"ext:" + file + ":" + tag},
				"outboundTag": "direct",
			}},
		},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workDir, "lists-xray-test.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-test", "-c", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+DefaultDatDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRouterListsIndexAndExtract(t *testing.T) {
	tg := routertest.Current(t)
	ix, files := rtDatIndex(t)

	seenReal := map[string]bool{}
	type pick struct {
		file, tag string
		count     int
	}
	var bestMax, bestRef pick
	var firstSite, firstIP *FileInfo
	for i := range files {
		f := files[i]
		if f.Kind != KindSite && f.Kind != KindIP {
			t.Errorf("%s: вид файла %q", f.Name, f.Kind)
		}
		if seenReal[f.Name] {
			t.Errorf("%s: файл продублирован в каталоге", f.Name)
		}
		seenReal[f.Name] = true
		for _, a := range f.Aliases {
			if seenReal[a] {
				t.Errorf("%s: псевдоним %s показан отдельной записью", f.Name, a)
			}
			seenReal[a] = true
		}
		if f.Kind == KindSite && firstSite == nil {
			firstSite = &files[i]
		}
		if f.Kind == KindIP && firstIP == nil {
			firstIP = &files[i]
		}

		tags, err := ix.Tags(f.Name)
		if err != nil {
			t.Errorf("%s: Tags: %v", f.Name, err)
			continue
		}
		withData := 0
		firstIdx := -1
		for i, ti := range tags {
			if ti.Count > 0 {
				withData++
				if firstIdx < 0 {
					firstIdx = i
				}
			}
			if ti.Count > bestMax.count {
				bestMax = pick{f.Name, ti.Tag, ti.Count}
			}
			if ti.Tag == "REFILTER" && ti.Count > bestRef.count {
				bestRef = pick{f.Name, ti.Tag, ti.Count}
			}
		}
		if len(tags) == 0 || withData == 0 {
			t.Errorf("%s: нет тегов с записями (тегов %d)", f.Name, len(tags))
			continue
		}

		first := tags[firstIdx]
		cat, err := ix.Extract(f.Name, first.Tag)
		if errors.Is(err, ErrListTooLarge) {
			t.Logf("%s:%s записей по индексу %d: выше предела (%d строк до отказа)", f.Name, first.Tag, first.Count, cat.Entries)
			continue
		}
		if err != nil {
			t.Errorf("%s:%s: Extract: %v", f.Name, first.Tag, err)
			continue
		}
		lines := append(append(append([]string{}, cat.Domains...), cat.Classical...), cat.CIDRs...)
		uniq := map[string]bool{}
		for _, l := range lines {
			uniq[l] = true
		}
		if cat.Entries != len(uniq) || len(uniq) != len(lines) {
			t.Errorf("%s:%s: Entries=%d, строк %d, уникальных %d", f.Name, first.Tag, cat.Entries, len(lines), len(uniq))
		}
		if cat.Entries > first.Count {
			t.Errorf("%s:%s: Entries=%d больше записей индекса %d", f.Name, first.Tag, cat.Entries, first.Count)
		}
		if cat.Hash == "" || cat.Kind != f.Kind {
			t.Errorf("%s:%s: Hash=%q Kind=%q", f.Name, first.Tag, cat.Hash, cat.Kind)
		}
		if cat.Heavy != (cat.Entries > WarnEntries) {
			t.Errorf("%s:%s: Heavy=%v при Entries=%d", f.Name, first.Tag, cat.Heavy, cat.Entries)
		}

		start := time.Now()
		again, err := ix.Extract(f.Name, first.Tag)
		if err != nil || again.Hash != cat.Hash {
			t.Errorf("%s:%s: повторная выгрузка: %v", f.Name, first.Tag, err)
		}
		if d := time.Since(start); d > 10*time.Millisecond {
			t.Errorf("%s:%s: повторная выгрузка не из кэша: %s", f.Name, first.Tag, d)
		}

		canonFile, canonTag, ok, err := ix.Has(f.Name, strings.ToLower(first.Tag))
		if err != nil || !ok || canonTag != first.Tag || canonFile != f.Name {
			t.Errorf("%s: Has(%q) = %q %q %v %v", f.Name, strings.ToLower(first.Tag), canonFile, canonTag, ok, err)
		}
		if _, err := ix.Extract(f.Name, "XCP-NO-SUCH-TAG"); !errors.Is(err, ErrListNotFound) {
			t.Errorf("%s: несуществующий тег: %v", f.Name, err)
		}
		if _, _, ok, _ := ix.Has(f.Name, "XCP-NO-SUCH-TAG"); ok {
			t.Errorf("%s: Has нашёл несуществующий тег", f.Name)
		}
		for _, a := range f.Aliases {
			if cf, _, ok, err := ix.Has(a, first.Tag); err != nil || !ok || cf != f.Name {
				t.Errorf("%s: Has по псевдониму %s: %q %v %v", f.Name, a, cf, ok, err)
			}
		}
		ex, err := ix.Examples(f.Name, first.Tag, 3)
		if err != nil || len(ex) == 0 {
			t.Errorf("%s:%s: Examples: %v %v", f.Name, first.Tag, ex, err)
		}
	}
	if _, err := ix.Tags("../etc/passwd.dat"); !errors.Is(err, ErrBadFileName) {
		t.Errorf("имя с «..» не отклонено: %v", err)
	}

	// Замер для A1/A2: самая большая категория (REFILTER, если есть).
	biggest := bestMax
	if bestRef.file != "" {
		biggest = bestRef
	}
	if biggest.file != "" {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		cat, err := ix.Extract(biggest.file, biggest.tag)
		took := time.Since(start)
		runtime.ReadMemStats(&after)
		t.Logf("замер %s: записей индекса %d, строк %d, время %s, рост кучи %d КБ, всего выделено %d КБ, GOARCH %s, ошибка %v",
			biggest.tag, biggest.count, cat.Entries, took, (int64(after.HeapAlloc)-int64(before.HeapAlloc))/1024,
			(int64(after.TotalAlloc)-int64(before.TotalAlloc))/1024, runtime.GOARCH, err)
	}

	// Сверка с Xray (D-21): настоящий тег проходит проверку, выдуманный — нет.
	bin := rtXrayBinary()
	if bin == "" {
		t.Log("xray не найден: сверка с ядром пропущена")
		return
	}
	for _, f := range []*FileInfo{firstSite, firstIP} {
		if f == nil {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil || len(tags) == 0 {
			continue
		}
		if out, err := rtXrayTest(t, bin, tg.WorkDir, f.Name, tags[0].Tag, f.Kind); err != nil {
			t.Errorf("xray -test с ext:%s:%s не прошёл: %v: %s", f.Name, tags[0].Tag, err, strings.TrimSpace(out))
		}
		out, err := rtXrayTest(t, bin, tg.WorkDir, f.Name, "XCP-NO-SUCH-TAG", f.Kind)
		if err == nil {
			t.Errorf("xray -test принял выдуманный тег %s:XCP-NO-SUCH-TAG", f.Name)
		} else if !strings.Contains(out, "failed to check code") && !strings.Contains(out, "code not found") {
			t.Errorf("xray -test отклонил выдуманный тег другим текстом: %s", strings.TrimSpace(out))
		}
	}
}

func TestRouterListsCatalog(t *testing.T) {
	routertest.Current(t)
	ix, _ := rtDatIndex(t)

	view, err := Resolve(ix, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	again, err := Resolve(ix, nil)
	if err != nil || !reflect.DeepEqual(view, again) {
		t.Errorf("Resolve недетерминирован или упал: %v", err)
	}
	if len(view.Lists) == 0 {
		t.Error("раздел «Все списки» пуст при установленных dat")
	}

	for _, s := range view.Services {
		if len(s.Refs) == 0 {
			t.Errorf("сервис %s показан без ссылок", s.ID)
		}
		for _, r := range s.Refs {
			if _, _, ok, err := ix.Has(r.File, r.Tag); err != nil || !ok {
				t.Errorf("сервис %s: ссылка %s · %s не найдена в индексе: %v", s.ID, r.File, r.Tag, err)
			}
		}
	}

	var ru *CountryView
	for i := range view.Countries {
		c := view.Countries[i]
		if c.IP == nil {
			t.Errorf("страна %s показана без IP-списка", c.Code)
			continue
		}
		if _, _, ok, err := ix.Has(c.IP.File, c.IP.Tag); err != nil || !ok {
			t.Errorf("страна %s: IP-ссылка %s · %s не найдена: %v", c.Code, c.IP.File, c.IP.Tag, err)
		}
		if c.Code == "RU" {
			ru = &view.Countries[i]
		}
	}
	if ru == nil {
		t.Error("RU нет в странах: ни geoip_zkeenip.dat, ни geoip_v2fly.dat с тегом RU")
	}

	// GRP-04: в доменных ссылках нет логики geoip:!cn — только теги файлов.
	checkTag := func(where, tag, kind string) {
		low := strings.ToLower(tag)
		if kind == KindSite && (strings.HasPrefix(low, "geoip:") || strings.HasPrefix(low, "ext:") || strings.HasPrefix(low, "geosite:")) {
			t.Errorf("%s: доменная ссылка с префиксом в теге %q", where, tag)
		}
	}
	for _, s := range catalog.Services {
		for _, r := range s.Refs {
			checkTag("service "+s.ID, r.Tag, r.Kind)
		}
	}
	for _, c := range catalog.Countries {
		for _, r := range append(append([]catRef{}, c.Site...), c.Blocked...) {
			checkTag("country "+c.Code, r.Tag, r.Kind)
		}
	}
	for _, s := range view.Services {
		for _, r := range s.Refs {
			checkTag("service "+s.ID, r.Tag, r.Kind)
		}
	}

	if len(view.Extended) == 0 {
		t.Error("нет расширенных наборов")
	}
	for _, e := range view.Extended {
		if !strings.HasPrefix(e.URL, "https://github.com/") || !strings.Contains(e.URL, "/releases/latest/download/") {
			t.Errorf("набор %s: адрес не GitHub release: %s", e.ID, e.URL)
		}
		if strings.Contains(e.URL, "..") || strings.ContainsAny(e.File, "/\\") {
			t.Errorf("набор %s: недопустимые файл или адрес", e.ID)
		}
	}
}
