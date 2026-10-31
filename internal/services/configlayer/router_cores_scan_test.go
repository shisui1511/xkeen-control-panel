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
	rtCoresLogName     = "01_log.json"
	rtCoresPolicyName  = "06_policy.json"
	rtCoresInbName     = "03_inbounds.json"
	rtCoresAPIName     = "00_api.json"
	rtCoresExtraName   = "05_extra.json"
	rtCoresShadowName  = "99_zz.json"
	rtCoresLogBody     = `{"log": {"loglevel": "warning"}}` + "\n"
	rtCoresPolicyBody  = `{"policy": {"levels": {"0": {"connIdle": 300}}, "system": {"statsInboundUplink": true}}}` + "\n"
	rtCoresShadowBody  = `{"dns": {"servers": ["127.0.0.3"]}}` + "\n"
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
