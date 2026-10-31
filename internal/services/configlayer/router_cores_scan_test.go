//go:build router

package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Скан чужих файлов Xray по ключам верхнего уровня на устройстве: настоящие
// файлы во временном каталоге рабочего каталога теста и `xray run -test`
// роутера. Работающие ядра стенда не трогаются, тест ядро не читает и не
// переключает — метка ядра не нужна. Фикстуры без реальных адресов.

// rtCoresWriteDir раскладывает файлы по каталогу.
func rtCoresWriteDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// rtCoresXrayBin возвращает бинарник Xray роутера или пропускает сценарий.
func rtCoresXrayBin(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(rtRefreshXrayBin); err != nil {
		t.Skipf("Xray не установлен на устройстве: %v", err)
	}
	return rtRefreshXrayBin
}

// rtCoresXray запускает Xray с аргументами и возвращает вывод и ошибку.
func rtCoresXray(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, rtCoresXrayBin(t), args...).CombinedOutput()
}

// rtCoresXrayTest проверяет каталог ядром: `xray run -test -confdir`, код 0.
func rtCoresXrayTest(t *testing.T, dir string) {
	t.Helper()
	if out, err := rtCoresXray(t, "run", "-test", "-confdir", dir); err != nil {
		t.Fatalf("xray run -test -confdir (%s): %v\n%s", filepath.Base(dir), err, out)
	}
}

// rtCoresCompact приводит JSON к компактному виду для сравнения.
func rtCoresCompact(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("json.Compact: %v: %s", err, raw)
	}
	return buf.String()
}

const (
	rtCoresDNSName      = "02_dns.json"
	rtCoresMixedName    = "00_mixed.json"
	rtCoresBadName      = "07_bad.json"
	rtCoresOutboundName = "04_outbounds.json"

	// Чистый файл dns с комментарием JSONC перед объектом.
	rtCoresDNSBody = "// комментарий владельца\n" +
		`{"dns": {"servers": ["127.0.0.1"], "queryStrategy": "UseIPv4"}}` + "\n"
	rtCoresDNSValue = `{"servers":["127.0.0.1"],"queryStrategy":"UseIPv4"}`
	// Смешанный файл: log и stats.
	rtCoresMixedBody = `{"log": {"loglevel": "warning"}, "stats": {}}` + "\n"
	// Файл с висячей запятой после комментария: не разбирается.
	rtCoresBadBody = "// сломанный файл\n" +
		`{"dns": {"servers": ["127.0.0.2"],},}` + "\n"
	rtCoresOutboundBody = `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}` + "\n"
)

func TestRouterCoresScanXray(t *testing.T) {
	rtCoresXrayBin(t)
	base := rtLiveDir(t)
	all := filepath.Join(base, "all")
	valid := filepath.Join(base, "valid")
	files := map[string]string{
		rtCoresDNSName:      rtCoresDNSBody,
		rtCoresMixedName:    rtCoresMixedBody,
		rtCoresOutboundName: rtCoresOutboundBody,
	}
	rtCoresWriteDir(t, valid, files)
	files[rtCoresBadName] = rtCoresBadBody
	rtCoresWriteDir(t, all, files)

	scanned, err := ScanXrayKeyFiles(all, nil)
	if err != nil {
		t.Fatalf("ScanXrayKeyFiles: %v", err)
	}
	byName := map[string]XrayKeyFile{}
	for _, f := range scanned {
		byName[f.Name] = f
	}
	// 04_outbounds.json — тоже чужой файл каталога (он без ключей секций).
	if len(scanned) != 4 {
		t.Fatalf("файлов в скане %d, ожидалось 4", len(scanned))
	}
	names := make([]string, 0, len(scanned))
	for _, f := range scanned {
		names = append(names, f.Name)
	}
	if !slices.IsSorted(names) {
		t.Errorf("файлы скана не по порядку имён: %v", names)
	}
	bad := byName[rtCoresBadName]
	if !bad.Broken {
		t.Errorf("%s не помечен как неразобранный", rtCoresBadName)
	}
	if !slices.Contains(bad.Keys, "dns") {
		t.Errorf("у %s терпимый просмотр не нашёл ключ dns: %v", rtCoresBadName, bad.Keys)
	}
	if dns := byName[rtCoresDNSName]; dns.Broken || !dns.Comments || dns.Hash == "" {
		t.Errorf("%s: Broken=%v Comments=%v Hash=%q", rtCoresDNSName, dns.Broken, dns.Comments, dns.Hash)
	}

	sec := ScanXraySections(scanned)["dns"]
	if !slices.Equal(sec.Pure, []string{rtCoresDNSName}) {
		t.Errorf("dns Pure=%v, ожидался [%s]", sec.Pure, rtCoresDNSName)
	}
	if !slices.Contains(sec.Comments, rtCoresDNSName) {
		t.Errorf("dns Comments=%v без %s", sec.Comments, rtCoresDNSName)
	}
	if !slices.Contains(sec.Mixed, rtCoresBadName) {
		t.Errorf("dns Mixed=%v без %s", sec.Mixed, rtCoresBadName)
	}
	if got := rtCoresCompact(t, sec.Effective); got != rtCoresDNSValue {
		t.Errorf("dns Effective=%s, ожидалось %s", got, rtCoresDNSValue)
	}
	// Неразобранный файл не «чистый» и владельца делает пользователем.
	if sec.Owner != OwnerUser {
		t.Errorf("dns Owner=%s с неразобранным файлом, ожидался %s", sec.Owner, OwnerUser)
	}

	// Без неразобранного файла: все файлы dns — из шаблонов XKeen.
	clean, err := ScanXrayKeyFiles(valid, nil)
	if err != nil {
		t.Fatalf("ScanXrayKeyFiles(valid): %v", err)
	}
	if got := ScanXraySections(clean)["dns"]; got.Owner != OwnerXKeen {
		t.Errorf("dns Owner=%s, ожидался %s", got.Owner, OwnerXKeen)
	}
	// Фикстуры валидны для ядра стенда.
	rtCoresXrayTest(t, valid)
}
