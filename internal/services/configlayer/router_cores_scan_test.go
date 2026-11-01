//go:build router

package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

	rtCoresScanAllSections(t, filepath.Join(base, "full"), files)
}

const (
	rtCoresLogName    = "01_log.json"
	rtCoresPolicyName = "06_policy.json"
	rtCoresInbName    = "03_inbounds.json"
	rtCoresAPIName    = "00_api.json"
	rtCoresExtraName  = "05_extra.json"
	rtCoresShadowName = "99_zz.json"
	rtCoresLogBody    = `{"log": {"loglevel": "warning"}}` + "\n"
	rtCoresPolicyBody = `{"policy": {"levels": {"0": {"connIdle": 300}}, "system": {"statsInboundUplink": true}}}` + "\n"
	// Смешанный файл (dns и stats) после файла панели: остаётся на месте и перекрывает его.
	// Чистый файл так же поздно по имени при взятии уходит в копии и не перекрывает.
	rtCoresShadowBody  = `{"dns": {"servers": ["127.0.0.3"]}, "stats": {}}` + "\n"
	rtCoresShadowValue = `{"servers":["127.0.0.3"]}`
	// Прозрачные входящие: redirect, tproxy и входящий XKeen force-proxy-redirect.
	rtCoresInbBody = `{"inbounds": [
  {"tag": "redirect", "listen": "127.0.0.1", "port": 61219, "protocol": "dokodemo-door",
   "settings": {"network": "tcp", "followRedirect": true}},
  {"tag": "tproxy", "listen": "127.0.0.1", "port": 61220, "protocol": "dokodemo-door",
   "settings": {"network": "udp", "followRedirect": true},
   "streamSettings": {"sockopt": {"tproxy": "tproxy"}}},
  {"tag": "force-proxy-redirect", "listen": "127.0.0.1", "port": 61221, "protocol": "dokodemo-door",
   "settings": {"network": "tcp", "followRedirect": true}}
]}
`
	// Смешанный файл блока api: api, stats, входящий api и правило api.
	rtCoresAPIBody = `{
  "api": {"tag": "api", "services": ["HandlerService", "StatsService"]},
  "stats": {},
  "inbounds": [
    {"tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}
  ],
  "routing": {"rules": [{"type": "field", "inboundTag": ["api"], "outboundTag": "api"}]}
}
`
	// Смешанный файл: inbounds и stats.
	rtCoresExtraBody = `{"inbounds": [{"tag": "extra", "listen": "127.0.0.1", "port": 61230, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1", "network": "tcp"}}], "stats": {}}` + "\n"
)

// rtCoresInboundTags возвращает теги входящих и режимы прозрачных входящих.
func rtCoresInboundTags(t *testing.T, raw json.RawMessage) (tags []string, modes map[string]string) {
	t.Helper()
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("inbounds Effective не массив: %v: %s", err, raw)
	}
	modes = map[string]string{}
	for _, item := range arr {
		var in struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(item, &in); err != nil {
			t.Fatalf("входящий: %v", err)
		}
		tags = append(tags, in.Tag)
		if mode, ok := IsTransparentInbound(item); ok {
			modes[in.Tag] = mode
		}
	}
	return tags, modes
}

// rtCoresScanAllSections проверяет все пять секций Xray, факты для генераторов и
// ядро на каталоге без неразобранного файла.
func rtCoresScanAllSections(t *testing.T, dir string, base map[string]string) {
	t.Helper()
	files := map[string]string{
		rtCoresLogName:    rtCoresLogBody,
		rtCoresPolicyName: rtCoresPolicyBody,
		rtCoresInbName:    rtCoresInbBody,
		rtCoresAPIName:    rtCoresAPIBody,
		rtCoresExtraName:  rtCoresExtraBody,
		rtCoresShadowName: rtCoresShadowBody,
	}
	for name, body := range base {
		if name != rtCoresBadName {
			files[name] = body
		}
	}
	rtCoresWriteDir(t, dir, files)
	scanned, err := ScanXrayKeyFiles(dir, nil)
	if err != nil {
		t.Fatalf("ScanXrayKeyFiles: %v", err)
	}
	sections := ScanXraySections(scanned)

	if got := sections["log"]; !slices.Equal(got.Pure, []string{rtCoresLogName}) || !slices.Equal(got.Mixed, []string{rtCoresMixedName}) {
		t.Errorf("log: Pure=%v Mixed=%v", got.Pure, got.Mixed)
	}
	if got := sections["policy"]; !slices.Equal(got.Pure, []string{rtCoresPolicyName}) || got.Owner != OwnerXKeen {
		t.Errorf("policy: Pure=%v Owner=%s", got.Pure, got.Owner)
	}
	dns := sections["dns"]
	if !slices.Equal(dns.Shadowing, []string{rtCoresShadowName}) {
		t.Errorf("dns Shadowing=%v, ожидался [%s]", dns.Shadowing, rtCoresShadowName)
	}
	if got := rtCoresCompact(t, dns.Effective); got != rtCoresShadowValue {
		t.Errorf("dns Effective=%s, ожидалось значение позднего файла %s", got, rtCoresShadowValue)
	}

	inb := sections["inbounds"]
	if !inb.InboundsMixed {
		t.Errorf("inbounds: InboundsMixed=false при смешанных %v", inb.Mixed)
	}
	if !slices.Contains(inb.Pure, rtCoresInbName) || !slices.Contains(inb.Mixed, rtCoresExtraName) || !slices.Contains(inb.Mixed, rtCoresAPIName) {
		t.Errorf("inbounds: Pure=%v Mixed=%v", inb.Pure, inb.Mixed)
	}
	if !slices.Equal(inb.ForceProxyTags, []string{"force-proxy-redirect"}) {
		t.Errorf("inbounds ForceProxyTags=%v, ожидался [force-proxy-redirect]", inb.ForceProxyTags)
	}
	if len(inb.Shadowing) != 0 {
		t.Errorf("inbounds Shadowing=%v: входящие объединяются, перекрытия нет", inb.Shadowing)
	}
	tags, modes := rtCoresInboundTags(t, inb.Effective)
	if want := []string{"api", "redirect", "tproxy", "force-proxy-redirect", "extra"}; !slices.Equal(tags, want) {
		// Файлы идут по имени: 00_api.json, 03_inbounds.json, 05_extra.json.
		t.Errorf("inbounds Effective теги=%v, ожидались %v", tags, want)
	}
	if modes["redirect"] != "redirect" || modes["tproxy"] != "tproxy" {
		t.Errorf("прозрачные входящие: %v, ожидались redirect и tproxy", modes)
	}
	if _, api := modes["api"]; api {
		t.Errorf("входящий api принят за прозрачный")
	}

	other := sections["other"]
	if len(other.Pure) != 0 {
		t.Errorf("other Pure=%v: stats лежит только в смешанных файлах", other.Pure)
	}
	if !slices.Contains(other.Mixed, rtCoresAPIName) || !slices.Contains(other.Mixed, rtCoresExtraName) || !slices.Contains(other.Mixed, rtCoresMixedName) {
		t.Errorf("other Mixed=%v", other.Mixed)
	}
	if got := rtCoresCompact(t, other.Effective); got != `{"stats":{}}` {
		t.Errorf("other Effective=%s", got)
	}

	facts := XrayFacts(scanned)
	if !facts.ForeignAPI || facts.Observatory {
		t.Errorf("XrayFacts: ForeignAPI=%v Observatory=%v, ожидалось true и false", facts.ForeignAPI, facts.Observatory)
	}
	if !slices.Equal(facts.ForceProxyTags, []string{"force-proxy-redirect"}) {
		t.Errorf("XrayFacts ForceProxyTags=%v", facts.ForceProxyTags)
	}
	// Файл api убран: чужого ключа api больше нет.
	if err := os.Remove(filepath.Join(dir, rtCoresAPIName)); err != nil {
		t.Fatal(err)
	}
	after, err := ScanXrayKeyFiles(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if XrayFacts(after).ForeignAPI {
		t.Errorf("XrayFacts: ForeignAPI=true без файла api")
	}
	// Фикстуры валидны для ядра стенда: каталог без неразобранного файла.
	rtCoresWriteDir(t, dir, map[string]string{rtCoresAPIName: rtCoresAPIBody})
	rtCoresXrayTest(t, dir)
}

const rtCoresMihomoProfile = "mode: rule\n" +
	"redir-port: 7000\n" +
	"tproxy-port: 7001\n" +
	"rules:\n" +
	"  - MATCH,DIRECT\n"

func TestRouterCoresScanMihomoActive(t *testing.T) {
	if _, err := os.Stat(rtMhBin); err != nil {
		t.Skipf("Mihomo не установлен на устройстве: %v", err)
	}
	base := rtLiveDir(t)
	root := filepath.Join(base, "mihomo")
	profile := filepath.Join(root, "profiles", "user.yaml")
	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte(rtCoresMihomoProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "config.yaml")
	if err := os.Symlink(profile, cfg); err != nil {
		t.Fatal(err)
	}
	check := func(label, wantName string) {
		t.Helper()
		name, data, err := ReadMihomoActive(root)
		if err != nil {
			t.Fatalf("%s: ReadMihomoActive: %v", label, err)
		}
		if name != wantName {
			t.Errorf("%s: имя %q, ожидалось %q", label, name, wantName)
		}
		if string(data) != rtCoresMihomoProfile {
			t.Errorf("%s: байты профиля не совпали", label)
		}
	}
	check("симлинк (абсолютный)", "profiles/user.yaml")

	// Симлинк с относительной целью.
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("profiles", "user.yaml"), cfg); err != nil {
		t.Fatal(err)
	}
	check("симлинк (относительный)", "profiles/user.yaml")

	// Цель за пределами корня не читается.
	outside := filepath.Join(base, "outside.yaml")
	if err := os.WriteFile(outside, []byte(rtCoresMihomoProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, cfg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadMihomoActive(root); !errors.Is(err, ErrSymlinkOutsideRoot) {
		t.Errorf("симлинк за пределы корня: ошибка %v, ожидалась ErrSymlinkOutsideRoot", err)
	}

	// Обычный файл.
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(rtCoresMihomoProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	check("обычный файл", "config.yaml")

	// Фикстура валидна для ядра стенда.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, rtMhBin, "-t", "-d", root, "-f", cfg).CombinedOutput(); err != nil {
		t.Fatalf("mihomo -t: %v\n%s", err, out)
	}
}

// rtCoresDump сливает каталог ядром: `xray run -dump -confdir`, возвращает
// слитый конфиг. Xray без флага -dump пропускает сценарий.
func rtCoresDump(t *testing.T, dir string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, rtCoresXrayBin(t), "run", "-dump", "-confdir", dir)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if bytes.Contains(stderr.Bytes(), []byte("flag provided but not defined")) {
			t.Skipf("Xray стенда без флага -dump: %s", bytes.TrimSpace(stderr.Bytes()))
		}
		t.Fatalf("xray run -dump -confdir (%s): %v\n%s", filepath.Base(dir), err, stderr.Bytes())
	}
	out := stdout.Bytes()
	start := bytes.IndexByte(out, '{')
	if start < 0 {
		t.Fatalf("xray run -dump: в выводе нет конфига (%d байт)", len(out))
	}
	var merged map[string]any
	if err := json.NewDecoder(bytes.NewReader(out[start:])).Decode(&merged); err != nil {
		t.Fatalf("xray run -dump: вывод не JSON: %v", err)
	}
	return merged
}

// rtCoresDig достаёт значение по пути ключей объектов слитого конфига.
func rtCoresDig(root map[string]any, path ...string) (any, bool) {
	var cur any = root
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// rtCoresDecode разбирает JSON в общий вид для сравнения по значению.
func rtCoresDecode(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("JSON: %v: %s", err, raw)
	}
	return v
}

// TestRouterCoresMergeSemantics проверяет допущение A1: как Xray сливает log,
// dns, policy и inbounds из нескольких файлов каталога и совпадает ли это со
// сканом панели (действующее значение). Расхождение — находка для владельца:
// модель «поздний файл заменяет ключ целиком» тогда неверна.
func TestRouterCoresMergeSemantics(t *testing.T) {
	rtCoresXrayBin(t)
	base := rtLiveDir(t)

	// Ключ K: ранний 01_a.json задаёт поле, которого нет в позднем 02_b.json.
	type mergeCase struct {
		key, early, late, lateValue string
	}
	cases := []mergeCase{
		{
			key:       "log",
			early:     `{"log": {"loglevel": "debug", "dnsLog": true}}`,
			late:      `{"log": {"loglevel": "warning"}}`,
			lateValue: `{"loglevel":"warning"}`,
		},
		{
			key:       "dns",
			early:     `{"dns": {"queryStrategy": "UseIPv4", "servers": ["127.0.0.1"]}}`,
			late:      `{"dns": {"servers": ["127.0.0.2"]}}`,
			lateValue: `{"servers":["127.0.0.2"]}`,
		},
		{
			key:       "policy",
			early:     `{"policy": {"levels": {"0": {"connIdle": 111}}, "system": {"statsInboundUplink": true}}}`,
			late:      `{"policy": {"levels": {"0": {"handshake": 7}}}}`,
			lateValue: `{"levels":{"0":{"handshake":7}}}`,
		},
	}
	for _, c := range cases {
		dir := filepath.Join(base, "merge-"+c.key)
		rtCoresWriteDir(t, dir, map[string]string{"01_a.json": c.early + "\n", "02_b.json": c.late + "\n"})
		merged := rtCoresDump(t, dir)
		// Дамп ядра нормализован (все поля со значениями по умолчанию), поэтому
		// «поле осталось» по наличию ключа не проверить: арбитр — дамп каталога,
		// в котором лежит один поздний файл. Совпадение значит, что поздний файл
		// заменил ключ целиком.
		lateOnly := filepath.Join(base, "merge-"+c.key+"-late")
		rtCoresWriteDir(t, lateOnly, map[string]string{"02_b.json": c.late + "\n"})
		want, ok := rtCoresDig(rtCoresDump(t, lateOnly), c.key)
		if !ok {
			t.Fatalf("%s: в дампе каталога с одним поздним файлом нет ключа", c.key)
		}
		if got, ok := rtCoresDig(merged, c.key); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("слияние %s: ядро сливает не «поздний заменяет целиком»: два файла %v, один поздний %v", c.key, got, want)
		}
		scanned, err := ScanXrayKeyFiles(dir, nil)
		if err != nil {
			t.Fatalf("ScanXrayKeyFiles(%s): %v", c.key, err)
		}
		eff := ScanXraySections(scanned)[c.key].Effective
		if got := rtCoresCompact(t, eff); got != c.lateValue {
			t.Errorf("%s: Effective=%s, ожидался объект позднего файла %s", c.key, got, c.lateValue)
		}
	}

	rtCoresMergeInbounds(t, filepath.Join(base, "merge-inbounds"))
	rtCoresMergeAPIStats(t, filepath.Join(base, "merge-api"))
}

// rtCoresMergeInbounds — входящие объединяются по tag: поздний заменяет ранний
// на его месте, новые дописываются в конец.
func rtCoresMergeInbounds(t *testing.T, dir string) {
	t.Helper()
	const (
		early = `{"inbounds": [
  {"tag": "x", "listen": "127.0.0.1", "port": 20001, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}, "sniffing": {"enabled": true}},
  {"tag": "y", "listen": "127.0.0.1", "port": 20002, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}
]}`
		late = `{"inbounds": [
  {"tag": "x", "listen": "127.0.0.1", "port": 20003, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}
]}`
	)
	rtCoresWriteDir(t, dir, map[string]string{"01_a.json": early + "\n", "02_b.json": late + "\n"})
	type inbound struct {
		Tag      string          `json:"tag"`
		Port     int             `json:"port"`
		Sniffing json.RawMessage `json:"sniffing"`
	}
	read := func(label string, v any) []inbound {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		var list []inbound
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatalf("%s: inbounds не массив входящих: %v", label, err)
		}
		return list
	}
	merged := rtCoresDump(t, dir)
	raw, ok := merged["inbounds"]
	if !ok {
		t.Fatalf("inbounds: в слитом конфиге ядра нет ключа")
	}
	kernel := read("ядро", raw)
	if len(kernel) != 2 || kernel[0].Tag != "x" || kernel[0].Port != 20003 || kernel[1].Tag != "y" || kernel[1].Port != 20002 {
		t.Errorf("inbounds в слитом конфиге ядра: %+v, ожидались x:20003 и y:20002 по порядку", kernel)
	}
	// Что делает ядро с полями раннего входящего с тем же tag: заменяет целиком
	// или сливает вглубь — только наблюдение, вердикт модели по портам и тегам.
	if len(kernel) > 0 {
		t.Logf("входящий x: поле sniffing раннего файла сохранено ядром: %v", len(kernel[0].Sniffing) > 0)
	}
	scanned, err := ScanXrayKeyFiles(dir, nil)
	if err != nil {
		t.Fatalf("ScanXrayKeyFiles: %v", err)
	}
	var effective any
	if err := json.Unmarshal(ScanXraySections(scanned)["inbounds"].Effective, &effective); err != nil {
		t.Fatalf("inbounds Effective: %v", err)
	}
	panel := read("скан", effective)
	if len(panel) != len(kernel) {
		t.Errorf("inbounds: скан даёт %d входящих, ядро %d", len(panel), len(kernel))
		return
	}
	for i := range panel {
		if panel[i].Tag != kernel[i].Tag || panel[i].Port != kernel[i].Port {
			t.Errorf("inbounds[%d]: скан %s:%d, ядро %s:%d", i, panel[i].Tag, panel[i].Port, kernel[i].Tag, kernel[i].Port)
		}
	}
}

// rtCoresMergeAPIStats — файл api без policy не стирает policy.levels другого
// файла (143 Q5), а stats в двух файлах не мешает запуску.
func rtCoresMergeAPIStats(t *testing.T, dir string) {
	t.Helper()
	rtCoresWriteDir(t, dir, map[string]string{
		"00_out.json": rtCoresOutboundBody,
		"01_a.json":   `{"policy": {"levels": {"0": {"connIdle": 111}}}}` + "\n",
		"99_c.json": `{"api": {"tag": "api", "listen": "127.0.0.1:10085", "services": ["HandlerService", "StatsService"]}, "stats": {}}` +
			"\n",
	})
	merged := rtCoresDump(t, dir)
	if v, ok := rtCoresDig(merged, "policy", "levels", "0", "connIdle"); !ok || v != float64(111) {
		t.Errorf("файл api стёр policy.levels другого файла: connIdle=%v найден=%v", v, ok)
	}
	rtCoresXrayTest(t, dir)
	// stats в двух файлах: в файле api и в «Прочих ключах».
	rtCoresWriteDir(t, dir, map[string]string{"50_s.json": `{"stats": {}}` + "\n"})
	rtCoresXrayTest(t, dir)
}

// Снимок слоя на переходе владения читает активный профиль Mihomo (WR-02):
// профиль больше предела размера не читается и не разбирается, сторона
// считается непрочитанной (объявление port), а профиль в пределах читается и
// даёт факты. Настоящие файлы во временном каталоге теста; ядро стенда не
// трогается, метка ядра не нужна.
func TestRouterCoresRestartProfileLimit(t *testing.T) {
	root := filepath.Join(rtLiveDir(t), "mihomo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "config.yaml")
	draft := Sections{
		SectionMihomoMain:     rtRaw(`{"v":1,"managed":true,"value":{"redir-port":7892,"tproxy-port":7893}}`),
		SectionMihomoInbounds: rtRaw(`{"v":1,"managed":true,"value":{"tun":{"enable":false}}}`),
	}
	roots := Roots{Mihomo: root}

	small := "redir-port: 7892\ntproxy-port: 7893\ntun:\n  enable: false\n"
	if err := os.WriteFile(cfg, []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := mihomoTakeTransition(roots, draft)
	if tr == nil || tr.unread {
		t.Fatalf("профиль в пределах размера: переход %+v, ожидалось прочитанное", tr)
	}

	// Корректный YAML больше предела: не читается и не разбирается целиком.
	var big bytes.Buffer
	big.WriteString(small)
	big.WriteString("proxies:\n")
	line := "  - {name: xcp-fixture, type: direct}\n"
	for big.Len() <= MaxMihomoProfileBytes+len(line) {
		big.WriteString(line)
	}
	if err := os.WriteFile(cfg, big.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	tr = mihomoTakeTransition(roots, draft)
	if tr == nil || !tr.unread {
		t.Errorf("профиль больше %d байт: переход %+v, ожидалась непрочитанная сторона", MaxMihomoProfileBytes, tr)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("переход по профилю больше предела занял %s", d)
	}
}
