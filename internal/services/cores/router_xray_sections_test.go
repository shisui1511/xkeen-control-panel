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

// Значения остальных секций: поля вне формы (log.access и log.error, policy
// statsUserUplink, sniffing и streamSettings входящих) и «Прочие ключи».
const (
	rtXraySecLogValue = `{"loglevel":"warning","access":"/opt/var/log/xray/access.log","error":"/opt/var/log/xray/error.log","dnsLog":false}`
	// statsInboundUplink выключен в значении: панель закрепляет его включённым.
	rtXraySecPolicyValue = `{"levels":{"0":{"handshake":4,"connIdle":300,"uplinkOnly":2,"downlinkOnly":5,"statsUserUplink":true}},` +
		`"system":{"statsInboundUplink":false,"statsInboundDownlink":true}}`
	rtXraySecPolicyWant = `{"levels":{"0":{"handshake":4,"connIdle":300,"uplinkOnly":2,"downlinkOnly":5,"statsUserUplink":true}},` +
		`"system":{"statsInboundUplink":true,"statsInboundDownlink":true,"statsOutboundUplink":true,"statsOutboundDownlink":true}}`
	rtXraySecInboundsValue = `[` +
		`{"tag":"redirect","port":1181,"protocol":"dokodemo-door","settings":{"network":"tcp","followRedirect":true},"sniffing":{"enabled":true,"destOverride":["http","tls"]}},` +
		`{"tag":"tproxy","port":1181,"protocol":"dokodemo-door","settings":{"network":"udp","followRedirect":true},"streamSettings":{"sockopt":{"tproxy":"tproxy"}},"sniffing":{"enabled":true,"destOverride":["http","tls"]}},` +
		`{"tag":"socks-in","listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{"auth":"noauth","udp":true},"sniffing":{"enabled":true,"destOverride":["http","tls"]}}` +
		`]`
	rtXraySecOtherValue = `{"stats":{},"fakedns":{"ipPool":"198.18.0.0/15","poolSize":65535}}`
)

// rtXraySecAll — все пять секций managed со значениями.
func rtXraySecAll(t *testing.T) configlayer.Sections {
	t.Helper()
	return configlayer.Sections{
		configlayer.SectionXrayLog:      rtXraySecSection(t, rtXraySecLogValue),
		configlayer.SectionXrayDNS:      rtXraySecSection(t, rtXraySecDNSValue),
		configlayer.SectionXrayInbounds: rtXraySecSection(t, rtXraySecInboundsValue),
		configlayer.SectionXrayPolicy:   rtXraySecSection(t, rtXraySecPolicyValue),
		configlayer.SectionXrayOther:    rtXraySecSection(t, rtXraySecOtherValue),
	}
}

// rtXraySecWant сверяет значение ключа файла панели с ожидаемым.
func rtXraySecWant(t *testing.T, files map[string][]byte, name, key, want string) {
	t.Helper()
	content, ok := files[name]
	if !ok {
		t.Fatalf("файла панели %s нет", name)
	}
	got := rtXraySecValue(t, content, key)
	if w := rtXraySecCompact(t, []byte(want)); got != w {
		t.Errorf("%s: значение %q изменилось:\n got: %s\nwant: %s", name, key, got, w)
	}
	if bytes.Contains(content, []byte(`"managed"`)) || bytes.Contains(content, []byte(`"keep"`)) {
		t.Errorf("%s: в файле ядра служебные поля панели", name)
	}
}

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
		rtXraySecWant(t, byName, configlayer.PanelXrayDNSFile, "dns", rtXraySecDNSValue)
	})

	t.Run("все секции", func(t *testing.T) {
		dir := rtXraySecDir(t)
		sections := rtXraySecAll(t)
		files := rtXraySecGenerate(t, sections)
		if len(files) != 5 {
			t.Fatalf("ожидалось пять файлов панели, получено %d", len(files))
		}
		again := rtXraySecGenerate(t, sections)
		for i := range files {
			if files[i].RelPath != again[i].RelPath || !bytes.Equal(files[i].Content, again[i].Content) {
				t.Errorf("вывод генератора недетерминирован: %s", files[i].RelPath)
			}
		}
		byName := rtXraySecWrite(t, dir, files)
		rtXraySecTest(t, dir)
		rtXraySecWant(t, byName, configlayer.PanelXrayLogFile, "log", rtXraySecLogValue)
		rtXraySecWant(t, byName, configlayer.PanelXrayDNSFile, "dns", rtXraySecDNSValue)
		rtXraySecWant(t, byName, configlayer.PanelXrayInboundsFile, "inbounds", rtXraySecInboundsValue)
		rtXraySecWant(t, byName, configlayer.PanelXrayPolicyFile, "policy", rtXraySecPolicyWant)
		other := byName[configlayer.PanelXrayOtherFile]
		if got, want := rtXraySecCompact(t, other), rtXraySecCompact(t, []byte(rtXraySecOtherValue)); got != want {
			t.Errorf("99_xcp-other.json:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("пустые прочие ключи", func(t *testing.T) {
		files := rtXraySecGenerate(t, configlayer.Sections{
			configlayer.SectionXrayOther: rtXraySecSection(t, `{}`),
		})
		if len(files) != 0 {
			t.Fatalf("пустые «Прочие ключи» не должны давать файл, получено %d", len(files))
		}
	})

	t.Run("не управляется и не установлен", func(t *testing.T) {
		raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: false, Value: json.RawMessage(rtXraySecDNSValue)})
		if err != nil {
			t.Fatal(err)
		}
		if files := rtXraySecGenerate(t, configlayer.Sections{configlayer.SectionXrayDNS: raw}); len(files) != 0 {
			t.Fatalf("секция без managed дала %d файлов", len(files))
		}
		files, err := XrayGenerator().Generate(configlayer.Input{Sections: rtXraySecAll(t), Mode: configlayer.SourceDraft}, configlayer.InstalledKernels{})
		if err != nil || len(files) != 0 {
			t.Fatalf("Xray не установлен: файлов %d, ошибка %v", len(files), err)
		}
	})

	t.Run("пресеты", func(t *testing.T) {
		dir := rtXraySecDir(t)
		sections := configlayer.Sections{}
		for _, spec := range XraySpecs {
			sections[spec.Section] = rtXraySecSection(t, "")
		}
		files := rtXraySecGenerate(t, sections)
		if len(files) != 4 {
			t.Fatalf("пресеты дают четыре файла (без «Прочих ключей»), получено %d", len(files))
		}
		byName := rtXraySecWrite(t, dir, files)
		rtXraySecTest(t, dir)
		rtXraySecWant(t, byName, configlayer.PanelXrayLogFile, "log", string(XrayPreset("log")))
		rtXraySecWant(t, byName, configlayer.PanelXrayDNSFile, "dns", string(XrayPreset("dns")))
		rtXraySecWant(t, byName, configlayer.PanelXrayInboundsFile, "inbounds", string(XrayPreset("inbounds")))
	})

}
