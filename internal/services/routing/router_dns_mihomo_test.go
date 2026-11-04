//go:build router

package routing

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Пути DNS профиля Mihomo под перехватом (148-10): генератор получает
// Live[dns_takeover] и собирает часть dns — слушатель, серверы через группу-цель,
// имена узлов и локальные зоны до туннеля. Профиль проверяет настоящий
// `mihomo -t`, пути — отдельный процесс Mihomo в каталоге теста: в копии
// профиля слушатель DNS переносится на свободный порт loopback, сокет
// контроллера и метка сокетов убираются.
//
// Метки ядра нет: ядро стенда не читается и не переключается, :53 стенда не
// трогается (резолвер прошивки — 127.0.0.1:53 без override). «Узел» — rtStartNode:
// соединение с адресом DoH в его журнале значит, что запрос прошёл через группу.
// В журнал идут только да/нет и числа.

const (
	// rtdmFirmware — резолвер прошивки стенда без override.
	rtdmFirmware = "127.0.0.1:53"
	// rtdmFirmwareAlt — другой адрес резолвера прошивки в Live (новое включение).
	rtdmFirmwareAlt = "127.0.0.1:5353"
	// rtdmDoHSeen2 — второй сервер D-17 в журнале узла.
	rtdmDoHSeen2 = "8.8.8.8:443"
	// rtdmUserDoH — свой сервер пользователя.
	rtdmUserDoH = "https://1.0.0.1/dns-query"
)

// rtdmLive — живые данные перехвата Mihomo.
func rtdmLive(svc, firmware string, ipv6 bool) configlayer.DNSTakeoverLive {
	return configlayer.DNSTakeoverLive{
		Enabled: true, Kernel: configlayer.KernelMihomo, FirmwareResolver: firmware,
		ServiceDomain: strings.TrimSuffix(svc, "."), IPv6: ipv6,
	}
}

// rtdmGen собирает файлы Mihomo по черновику окружения с заданным
// Live[dns_takeover] (как после включения перехвата).
func rtdmGen(t *testing.T, e *rtEnv, live configlayer.DNSTakeoverLive) ([]configlayer.GeneratedFile, error) {
	t.Helper()
	st := e.state(t)
	in := inputOf(st)
	raw, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	in.Live = configlayer.Sections{}
	for k, v := range st.Live {
		in.Live[k] = v
	}
	in.Live[configlayer.LiveKeyDNSTakeover] = raw
	return e.svc.generateMihomo(in, e.installed)
}

// rtdmMustGen — rtdmGen без ошибки генератора.
func rtdmMustGen(t *testing.T, e *rtEnv, live configlayer.DNSTakeoverLive) []configlayer.GeneratedFile {
	t.Helper()
	files, err := rtdmGen(t, e, live)
	if err != nil {
		t.Fatalf("генератор Mihomo под перехватом: %v", err)
	}
	return files
}

// rtdmProfile — текст профиля панели и часть dns из файлов генератора.
func rtdmProfile(t *testing.T, files []configlayer.GeneratedFile) (data []byte, dns map[string]any) {
	t.Helper()
	for _, f := range files {
		if f.RelPath != configlayer.PanelMihomoProfile {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal(f.Content, &doc); err != nil {
			t.Fatalf("профиль панели не YAML: %v", err)
		}
		dns, _ = doc["dns"].(map[string]any)
		if dns == nil {
			t.Fatal("в профиле нет части dns")
		}
		return f.Content, dns
	}
	t.Fatal("генератор не выдал профиль панели")
	return nil, nil
}

// rtdmPolicy — nameserver-policy части dns: ключ → значения строками.
func rtdmPolicy(dns map[string]any) map[string][]string {
	out := map[string][]string{}
	m, _ := dns["nameserver-policy"].(map[string]any)
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = []string{s}
			continue
		}
		out[k] = rtmStrings(v)
	}
	return out
}

// rtdmDir кладёт в каталог теста профиль как есть (profile.yaml), файлы
// провайдеров правил генератора и провайдеры узлов из «Применить» окружения.
func rtdmDir(t *testing.T, e *rtEnv, files []configlayer.GeneratedFile) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-dnsm-")
	for _, f := range files {
		name := f.RelPath
		if name == configlayer.PanelMihomoProfile {
			name = "profile.yaml"
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rtCopyFiles(t, e.mihomoPath(proxyProviderDir), filepath.Join(dir, proxyProviderDir))
	return dir
}

// rtdmCheck — профиль принят `mihomo -t`.
func rtdmCheck(t *testing.T, dir string) {
	t.Helper()
	if errLine := rtdsCheck(rtMihomoBin, []string{"-t", "-d", dir, "-f", filepath.Join(dir, "profile.yaml")}, nil); errLine != "" {
		t.Errorf("mihomo -t отверг профиль под перехватом: %s", errLine)
	}
}

// rtdmStart запускает отдельный Mihomo по копии профиля: socks готовности и
// слушатель DNS — на свободных портах loopback, без портов прозрачного прокси,
// метки сокетов и сокета контроллера стенда. Возвращает процесс и адрес DNS.
func rtdmStart(t *testing.T, dir string) (*rtProc, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("профиль не YAML: %v", err)
	}
	for _, k := range []string{"redir-port", "tproxy-port", "routing-mark", "external-controller-unix", "external-controller"} {
		delete(doc, k)
	}
	ready, dnsPort := rtFreePort(t), rtFreePort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
	doc["socks-port"] = ready
	doc["bind-address"] = "127.0.0.1"
	doc["allow-lan"] = false
	dns, _ := doc["dns"].(map[string]any)
	if dns == nil {
		t.Fatal("в профиле нет части dns")
	}
	dns["listen"] = addr
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	return rtStartMihomoCore(t, dir, ready), addr
}

// rtdmSection пишет в черновик секцию DNS Mihomo под управлением панели.
func rtdmSection(t *testing.T, e *rtEnv, value string) {
	t.Helper()
	raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: true, Value: json.RawMessage(value)})
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, configlayer.SectionMihomoDNS, raw)
}

// rtdmRestoreSection возвращает секцию DNS Mihomo черновика к значению до
// подтеста (нет секции — удаляет её).
func rtdmRestoreSection(t *testing.T, e *rtEnv) {
	t.Helper()
	orig, had := e.state(t).Draft[configlayer.SectionMihomoDNS]
	t.Cleanup(func() {
		if had {
			e.putSection(t, configlayer.SectionMihomoDNS, orig)
			return
		}
		if _, now := e.state(t).Draft[configlayer.SectionMihomoDNS]; now {
			e.dropSection(t, configlayer.SectionMihomoDNS)
		}
	})
}

// rtdmEqual — равенство списков строк по порядку.
func rtdmEqual(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}

func TestRouterDNSMihomoPaths(t *testing.T) {
	svc := rtdsServiceDomain()
	if !rtdsFirmwareAlive(svc, 10*time.Second) {
		t.Fatal("резолвер прошивки не отвечает на 127.0.0.1:53 до теста")
	}
	nodePort, nodeLog := rtStartNode(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rt-node")
	proxyG, ok := rtGroupByName(e.groups(t), defaultProxyGroupName)
	if !ok {
		t.Fatal("нет группы «Прокси» по умолчанию")
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: proxyG.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)

	// Без перехвата профиль собирается как раньше: слушателя :53 нет.
	plain, err := e.svc.generateMihomo(inputOf(e.state(t)), e.installed)
	if err != nil {
		t.Fatalf("генератор без перехвата: %v", err)
	}
	_, plainDNS := rtdmProfile(t, plain)
	if _, has := plainDNS["listen"]; has {
		t.Error("без перехвата в части dns появился listen")
	}

	files := rtdmMustGen(t, e, rtdmLive(svc, rtdmFirmware, false))
	_, dns := rtdmProfile(t, files)
	suffix := "#" + proxyG.Name
	ns := rtmStrings(dns["nameserver"])
	viaGroup, hasSystem := len(ns) > 0, false
	for _, s := range ns {
		viaGroup = viaGroup && strings.HasSuffix(s, suffix)
		hasSystem = hasSystem || s == "system" || strings.HasPrefix(s, "system#")
	}
	t.Logf("серверов nameserver: %d, все через группу: %s, есть system: %s", len(ns), rtxYesNo(viaGroup), rtxYesNo(hasSystem))
	if !viaGroup || hasSystem {
		t.Errorf("nameserver под перехватом: через группу %s, system %s", rtxYesNo(viaGroup), rtxYesNo(hasSystem))
	}
	if !rtdmEqual(ns, dnsViaGroup(dnsDefaultDoH, proxyG.Name)) {
		t.Errorf("nameserver при пресете секции — не умолчания D-17 через группу (%d записей)", len(ns))
	}
	// D-31 fact-closed: без IPv6 — 0.0.0.0:53, с IPv6 — один сокет [::]:53.
	if dns["listen"] != mihomoDNSListen4 {
		t.Errorf("dns.listen без IPv6 — не %s", mihomoDNSListen4)
	}
	if dns["enable"] != true {
		t.Error("dns.enable под перехватом не true")
	}
	_, dns6 := rtdmProfile(t, rtdmMustGen(t, e, rtdmLive(svc, rtdmFirmware, true)))
	if dns6["listen"] != mihomoDNSListen6 {
		t.Errorf("dns.listen с IPv6 — не %s", mihomoDNSListen6)
	}
	// D-18 race: пара DoH по IP и в конце резолвер прошивки из Live.
	psn := rtmStrings(dns["proxy-server-nameserver"])
	wantPSN := append(append([]string{}, dnsDefaultDoH...), "udp://"+rtdmFirmware)
	t.Logf("proxy-server-nameserver: %d записей, по решению D-18: %s", len(psn), rtxYesNo(rtdmEqual(psn, wantPSN)))
	if !rtdmEqual(psn, wantPSN) {
		t.Error("proxy-server-nameserver не равен [DoH, DoH, резолвер прошивки из Live]")
	}
	if len(rtmStrings(dns["default-nameserver"])) == 0 {
		t.Error("default-nameserver пуст")
	}
	policy := rtdmPolicy(dns)
	zonesOK := rtdmEqual(policy["+.lan"], []string{"udp://" + rtdmFirmware}) &&
		rtdmEqual(policy["+.10.in-addr.arpa"], []string{"udp://" + rtdmFirmware})
	if svc != "" {
		zonesOK = zonesOK && rtdmEqual(policy["+."+strings.TrimSuffix(svc, ".")], []string{"udp://" + rtdmFirmware})
	}
	t.Logf("записей nameserver-policy: %d, локальные зоны на прошивку: %s", len(policy), rtxYesNo(zonesOK))
	if !zonesOK {
		t.Error("локальные зоны и служебный домен не ведут на резолвер прошивки")
	}

	dir := rtdmDir(t, e, files)
	rtdmCheck(t, dir)
	p, addr := rtdmStart(t, dir)
	ext := rtdsResolveWait(addr, rtdsExtAlive, 20*time.Second)
	saw := rtWaitNodeSaw(nodeLog, rtdsDoHSeen, 5*time.Second) || rtNodeSaw(nodeLog, rtdmDoHSeen2)
	t.Logf("внешнее имя разрешилось: %s, запрос через узел группы: %s", rtxYesNo(ext.resolved()), rtxYesNo(saw))
	if !ext.resolved() || !saw {
		t.Errorf("внешнее имя через группу: ответ %s, узел видел DoH %s", rtxYesNo(ext.resolved()), rtxYesNo(saw))
	}
	if svc != "" {
		local := rtdsResolveWait(addr, svc, 10*time.Second)
		t.Logf("служебный домен разрешился: %s", rtxYesNo(local.resolved()))
		if !local.resolved() {
			t.Error("служебный домен роутера не разрешился через резолвер прошивки")
		}
	}

	// Адрес прошивки берётся только из Live: значение секции пользователя его не
	// замораживает, а новое включение даёт новый адрес.
	t.Run("firmware_live", func(t *testing.T) {
		rtdmRestoreSection(t, e)
		rtdmSection(t, e, `{"enable":true,"ipv6":false,`+
			`"nameserver":["system","127.0.0.1","`+rtdmUserDoH+`#Чужая&ecs=1.2.3.0/24"],`+
			`"proxy-server-nameserver":["`+rtdmUserDoH+`"]}`)
		_, first := rtdmProfile(t, rtdmMustGen(t, e, rtdmLive(svc, rtdmFirmware, false)))
		altFiles := rtdmMustGen(t, e, rtdmLive(svc, rtdmFirmwareAlt, false))
		_, alt := rtdmProfile(t, altFiles)
		wantFirst := []string{rtdmUserDoH, "udp://" + rtdmFirmware}
		wantAlt := []string{rtdmUserDoH, "udp://" + rtdmFirmwareAlt}
		gotFirst, gotAlt := rtmStrings(first["proxy-server-nameserver"]), rtmStrings(alt["proxy-server-nameserver"])
		t.Logf("proxy-server-nameserver с адресом из Live: первое включение %s, новое %s",
			rtxYesNo(rtdmEqual(gotFirst, wantFirst)), rtxYesNo(rtdmEqual(gotAlt, wantAlt)))
		if !rtdmEqual(gotFirst, wantFirst) || !rtdmEqual(gotAlt, wantAlt) {
			t.Error("proxy-server-nameserver: не [сервер пользователя, резолвер прошивки из Live]")
		}
		if got := rtmStrings(alt["nameserver"]); !rtdmEqual(got, []string{rtdmUserDoH + "#" + proxyG.Name}) {
			t.Errorf("nameserver пользователя: system и loopback не убраны или суффикс не заменён (%d записей)", len(got))
		}
		draft := string(e.state(t).Draft[configlayer.SectionMihomoDNS])
		if strings.Contains(draft, rtdmFirmware) || strings.Contains(draft, rtdmFirmwareAlt) {
			t.Error("адрес резолвера прошивки попал в значение секции черновика")
		}
		rtdmCheck(t, rtdmDir(t, e, altFiles))
	})

	// D-10: запасного direct нет — при мёртвой группе внешнее имя не разрешается,
	// служебный домен идёт через прошивку. Процесс перезапускается (пустой кеш).
	t.Run("dead_group", func(t *testing.T) {
		deadFiles := rtdmMustGen(t, e, rtdmLive(svc, rtdmFirmware, false))
		p.stop()
		stopped := rtdsStopNode(nodeLog)
		_, deadAddr := rtdmStart(t, rtdmDir(t, e, deadFiles))
		time.Sleep(time.Second)
		dead := rtdsQuery(deadAddr, rtdsExtDead, dnsmessage.TypeA, 6*time.Second)
		t.Logf("узел остановлен: %s; внешнее имя при мёртвой группе: %s за %s мс", rtxYesNo(stopped), dead.code(), dead.ms())
		if !stopped {
			t.Fatal("узел не остановлен")
		}
		if dead.resolved() {
			t.Error("внешнее имя разрешилось при мёртвой группе: запрос ушёл мимо туннеля")
		}
		if svc != "" {
			local := rtdsResolveWait(deadAddr, svc, 10*time.Second)
			t.Logf("служебный домен при мёртвой группе: %s", rtxYesNo(local.resolved()))
			if !local.resolved() {
				t.Error("служебный домен не разрешился при мёртвой группе")
			}
		}
	})
}
