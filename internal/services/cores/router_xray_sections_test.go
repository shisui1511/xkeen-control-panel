//go:build router

package cores

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Файлы секций Xray из значений черновика на устройстве: настоящий генератор,
// файлы во временном каталоге рабочего каталога теста и `xray run -test
// -confdir` роутера. Работающие ядра стенда не трогаются, тест ядро не читает и
// не переключает — метка ядра не нужна.

// rtXraySecBinPath — бинарник Xray на роутере.
const rtXraySecBinPath = "/opt/sbin/xray"

// rtXraySecOutbound — исходящий freedom рядом с файлами панели: без исходящего
// Xray каталог не принимает.
const rtXraySecOutbound = `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}` + "\n"

// rtXraySecBin возвращает бинарник Xray или пропускает сценарий.
func rtXraySecBin(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(rtXraySecBinPath); err != nil {
		t.Skipf("Xray не установлен на устройстве: %v", err)
	}
	return rtXraySecBinPath
}

// rtXraySecDir создаёт пустой каталог в рабочем каталоге теста.
func rtXraySecDir(t *testing.T) string {
	t.Helper()
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "cores-xray-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// rtXraySecTest проверяет каталог ядром: `xray run -test -confdir`, код 0.
func rtXraySecTest(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, rtXraySecBin(t), "run", "-test", "-confdir", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("xray run -test -confdir (%s): %v\n%s", filepath.Base(dir), err, out)
	}
}

// rtXraySecSection кодирует секцию черновика: managed с заданным значением.
func rtXraySecSection(t *testing.T, value string) json.RawMessage {
	t.Helper()
	cs := configlayer.CoreSection{Managed: true}
	if value != "" {
		cs.Value = json.RawMessage(value)
	}
	raw, err := configlayer.EncodeCoreSection(cs)
	if err != nil {
		t.Fatalf("EncodeCoreSection: %v", err)
	}
	return raw
}

// rtXraySecGenerate запускает настоящий генератор на секциях черновика.
func rtXraySecGenerate(t *testing.T, sections configlayer.Sections) []configlayer.GeneratedFile {
	t.Helper()
	files, err := XrayGenerator().Generate(configlayer.Input{Sections: sections, Mode: configlayer.SourceDraft}, configlayer.InstalledKernels{Xray: true})
	if err != nil {
		t.Fatalf("XrayGenerator: %v", err)
	}
	return files
}

// rtXraySecWrite раскладывает сгенерированные файлы и исходящий freedom по
// каталогу, возвращает файлы по имени.
func rtXraySecWrite(t *testing.T, dir string, files []configlayer.GeneratedFile) map[string][]byte {
	t.Helper()
	byName := map[string][]byte{}
	for _, f := range files {
		if f.Kernel != configlayer.KernelXray || f.Kind != configlayer.KindXrayJSON {
			t.Fatalf("файл %s: ядро %q, вид %q", f.RelPath, f.Kernel, f.Kind)
		}
		if err := os.WriteFile(filepath.Join(dir, f.RelPath), f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
		byName[f.RelPath] = f.Content
	}
	if err := os.WriteFile(filepath.Join(dir, "04_outbounds.json"), []byte(rtXraySecOutbound), 0o644); err != nil {
		t.Fatal(err)
	}
	return byName
}

// rtXraySecCompact приводит JSON к компактному виду.
func rtXraySecCompact(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("json.Compact: %v: %s", err, raw)
	}
	return buf.String()
}

// rtXraySecValue достаёт значение ключа верхнего уровня файла панели в
// компактном виде.
func rtXraySecValue(t *testing.T, content []byte, key string) string {
	t.Helper()
	o, err := configlayer.ParseOrderedObject(content)
	if err != nil {
		t.Fatalf("файл панели не JSON-объект: %v\n%s", err, content)
	}
	raw, ok := o.Get(key)
	if !ok {
		t.Fatalf("в файле панели нет ключа %q:\n%s", key, content)
	}
	return rtXraySecCompact(t, raw)
}

// rtXraySecDNSValue — значение секции DNS: сервер-строка, сервер-объект и поля,
// которых форма не знает ("tag").
const rtXraySecDNSValue = `{"servers":["127.0.0.1",{"address":"127.0.0.2","domains":["domain:example.test"],"port":5353}],` +
	`"queryStrategy":"UseIPv4","disableCache":true,"tag":"dns-in"}`

func TestRouterCoresXraySectionsGenerate(t *testing.T) {
	rtXraySecBin(t)

	t.Run("dns", func(t *testing.T) {
		dir := rtXraySecDir(t)
		files := rtXraySecGenerate(t, configlayer.Sections{
			configlayer.SectionXrayDNS: rtXraySecSection(t, rtXraySecDNSValue),
		})
		if len(files) != 1 || files[0].RelPath != configlayer.PanelXrayDNSFile {
			t.Fatalf("ожидался один файл %s, получено %d", configlayer.PanelXrayDNSFile, len(files))
		}
		byName := rtXraySecWrite(t, dir, files)
		rtXraySecTest(t, dir)
		got := rtXraySecValue(t, byName[configlayer.PanelXrayDNSFile], "dns")
		if want := rtXraySecCompact(t, []byte(rtXraySecDNSValue)); got != want {
			t.Errorf("значение dns в файле изменилось:\n got: %s\nwant: %s", got, want)
		}
	})
}
