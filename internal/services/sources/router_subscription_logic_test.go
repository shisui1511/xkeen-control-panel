//go:build router

package sources

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Инварианты подписок, которые дёшево проверить без провайдера: классификация
// отказов D-32, разбор ответа и заглушек, метаданные, снимки, наборы по ядрам и
// фиксация ожидающих снимков. Данные узлов — ссылки из testdata/links; вердикт —
// настоящие эмиттеры узлов и настоящая файловая система устройства.

// rtNodes читает узлы из ссылок testdata/links по именам файлов.
func rtNodes(t *testing.T, names ...string) []nodes.Node {
	t.Helper()
	out := make([]nodes.Node, 0, len(names))
	for _, name := range names {
		rec := nodes.Recognize(inputOf(rtLink(t, name)))
		if len(rec.Nodes) != 1 {
			t.Fatalf("ссылка %s: узлов %d", name, len(rec.Nodes))
		}
		out = append(out, rec.Nodes[0])
	}
	return out
}

// Счётчик «рабочих узлов» ответа считает полные дубли один раз: тот же узел в
// нескольких конфигурациях Xray-JSON не раздувает «N для Xray» против карточки.
func TestRouterUsableCountCollapsesDuplicates(t *testing.T) {
	one := rtNodes(t, "vless_ws")
	two := rtNodes(t, "vless_ws", "trojan_grpc")
	list := append(append(append([]nodes.Node{}, one...), one...), two...)
	if got := usableCount(list); got != 2 {
		t.Fatalf("узлов в ответе %d (с дублями), счётчик %d, нужно 2", len(list), got)
	}
}

func TestRouterClassifyTransportErrors(t *testing.T) {
	const token = "pathtoken9999"
	u := &url.URL{Scheme: "https", Host: "provider.invalid", Path: "/" + token}
	wrap := func(err error) error { return &url.Error{Op: "Get", URL: u.String(), Err: err} }
	cases := []struct {
		name      string
		err       error
		connected bool
		dnsOK     bool
		code      string
	}{
		{"dns", wrap(&net.DNSError{Err: "no such host", Name: "provider.invalid", IsNotFound: true}), false, false, nodes.CodeDNSFailed},
		{"tls", wrap(x509.UnknownAuthorityError{}), true, true, nodes.CodeTLSError},
		{"ssrf", wrap(fmt.Errorf("dial: %w", utils.ErrPrivateNetwork)), false, false, nodes.CodeSSRFBlocked},
		{"timeout after response started", wrap(context.DeadlineExceeded), true, true, nodes.CodeTimeout},
		{"timeout before dns", wrap(context.DeadlineExceeded), false, false, nodes.CodeTimeout},
		{"timeout after dns without connection", wrap(context.DeadlineExceeded), false, true, nodes.CodeUnreachableDirect},
		{"refused", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}), false, true, nodes.CodeUnreachableDirect},
		{"broken after connect", wrap(io.ErrUnexpectedEOF), true, true, nodes.CodeServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyTransportErr(tc.err, u, tc.connected, tc.dnsOK)
			if got == nil || got.Code != tc.code {
				t.Fatalf("код %v, нужен %s", got, tc.code)
			}
			if strings.Contains(got.Error(), token) || strings.Contains(got.Detail, token) {
				t.Error("токен из пути адреса попал в ошибку")
			}
			if strings.Contains(got.Detail, "https://provider.invalid/") {
				t.Error("в Detail остался исходный адрес")
			}
		})
	}

	for status, code := range map[int]string{401: nodes.CodeHTTPAuth, 403: nodes.CodeHTTPAuth, 404: nodes.CodeHTTPAuth, 500: nodes.CodeServerError, 502: nodes.CodeServerError} {
		got := classifyStatus(status)
		if got == nil || got.Code != code || got.HTTPStatus != status || got.Params["status"] != fmt.Sprint(status) {
			t.Errorf("статус %d: %+v", status, got)
		}
		if g := statusGroup(got.Code); (code == nodes.CodeHTTPAuth) != (g == StatusRejected) || (code == nodes.CodeServerError) != (g == StatusNetwork) {
			t.Errorf("группа статуса %d: %s", status, g)
		}
	}
	if classifyStatus(200) != nil {
		t.Error("200 не должен быть ошибкой")
	}
}

func TestRouterEvaluateBody(t *testing.T) {
	proxy := func(name, server string, port int, extra string) string {
		return fmt.Sprintf("  - {name: %s, type: ss, server: %s, port: %d, cipher: aes-128-gcm, password: secret-%s%s}\n", name, server, port, name, extra)
	}
	clash := func(parts ...string) []byte { return []byte("proxies:\n" + strings.Join(parts, "")) }

	t.Run("all stubs", func(t *testing.T) {
		p, serr := evaluateBody(clash(proxy("a", "0.0.0.0", 1, ""), proxy("b", "127.0.0.1", 443, "")), "")
		if serr == nil || serr.Code != nodes.CodeAllStubs || p.Stubs != 2 || len(p.Working) != 0 {
			t.Fatalf("одни заглушки: %+v %+v", serr, p)
		}
		if g := statusGroup(serr.Code); g != StatusRejected {
			t.Errorf("группа: %s", g)
		}
	})
	t.Run("working among stubs", func(t *testing.T) {
		p, serr := evaluateBody(clash(proxy("w1", "203.0.113.5", 8388, ""), proxy("w2", "203.0.113.6", 8388, ""),
			proxy("s1", "0.0.0.0", 1, ""), proxy("s2", "127.0.0.1", 443, ""), proxy("s3", "203.0.113.7", 1, "")), "")
		if serr != nil || len(p.Working) != 2 || p.Stubs != 3 || p.Total != 5 {
			t.Fatalf("рабочие среди заглушек: %+v %+v", serr, p)
		}
	})
	t.Run("empty", func(t *testing.T) {
		_, serr := evaluateBody([]byte("  \n"), "")
		if serr == nil || serr.Code != nodes.CodeEmptyResponse || statusGroup(serr.Code) != StatusProviderError {
			t.Fatalf("пустой ответ: %+v", serr)
		}
	})
	t.Run("html", func(t *testing.T) {
		for _, tc := range []struct{ body, ct string }{
			{"<!DOCTYPE html><html></html>", ""},
			{"<head><title>x</title></head>", ""},
			{"proxies:\n", "text/html; charset=utf-8"},
		} {
			_, serr := evaluateBody([]byte(tc.body), tc.ct)
			if serr == nil || serr.Code != nodes.CodeHTMLResponse || statusGroup(serr.Code) != StatusProviderError {
				t.Errorf("HTML %q: %+v", tc.ct, serr)
			}
		}
	})
}

func TestRouterFetchRequestHeaders(t *testing.T) {
	e := rtNewEnvWith(t, false, false, func(d *Deps) {
		d.Device = rtDevice
		d.HWID = func() string { return "A1B2C3D4E5F6" }
		d.KernelVersion = func(k string) string {
			if k == kernelMihomo {
				return "v1.19.38"
			}
			return ""
		}
	})
	ctx := context.Background()
	const target = "https://provider.invalid/sub"
	x, serr := e.svc.newFetchRequest(ctx, target, kernelXray, "A1B2C3D4E5F6", false)
	if serr != nil {
		t.Fatal(serr)
	}
	m, serr := e.svc.newFetchRequest(ctx, target, kernelMihomo, "A1B2C3D4E5F6", false)
	if serr != nil {
		t.Fatal(serr)
	}
	if x.Header.Get("User-Agent") != uaXray || m.Header.Get("User-Agent") != "ClashMeta/1.19.38; mihomo/1.19.38" {
		t.Errorf("User-Agent: %q и %q", x.Header.Get("User-Agent"), m.Header.Get("User-Agent"))
	}
	for _, h := range []string{"x-hwid", "x-device-os", "x-device-model", "x-ver-os"} {
		if x.Header.Get(h) == "" || x.Header.Get(h) != m.Header.Get(h) {
			t.Errorf("заголовок %s: %q и %q", h, x.Header.Get(h), m.Header.Get(h))
		}
	}
	locked, _ := e.svc.newFetchRequest(ctx, target, kernelXray, "A1B2C3D4E5F6", true)
	if locked.Header.Get("x-hwid") != "" || locked.Header.Get("x-device-os") == "" {
		t.Errorf("источник с x-hwid-not-supported: %v", locked.Header)
	}
	for _, bad := range []string{"ftp://provider.invalid/x", "file:///etc/passwd", "http:///x", "::"} {
		if _, serr := e.svc.newFetchRequest(ctx, bad, kernelXray, "", false); serr == nil || serr.Code != nodes.CodeInvalidURL {
			t.Errorf("адрес %q принят: %+v", bad, serr)
		}
	}

	// Свой HWID источника заменяет общий; x-hwid-not-supported отключает HWID.
	if h, lk := e.svc.sourceHWID(SubscriptionConfig{HWID: " CUSTOM0001 "}, Meta{}); h != "CUSTOM0001" || lk {
		t.Errorf("свой HWID: %q %v", h, lk)
	}
	if h, lk := e.svc.sourceHWID(SubscriptionConfig{}, Meta{}); h != "A1B2C3D4E5F6" || lk {
		t.Errorf("общий HWID: %q %v", h, lk)
	}
	if h, lk := e.svc.sourceHWID(SubscriptionConfig{HWID: "CUSTOM0001"}, Meta{HWIDLocked: true}); h != "" || !lk {
		t.Errorf("HWID при x-hwid-not-supported: %q %v", h, lk)
	}
}

// zeroReader бесконечно отдаёт нули и считает прочитанное.
type zeroReader struct{ n int64 }

func (z *zeroReader) Read(p []byte) (int, error) {
	z.n += int64(len(p))
	return len(p), nil
}

func TestRouterReadBodyLimit(t *testing.T) {
	const limit = 1 << 16
	zr := &zeroReader{}
	body, serr := readBody(zr, limit)
	if body != nil || serr == nil || serr.Code != nodes.CodeTooLarge {
		t.Fatalf("тело без конца: %+v", serr)
	}
	if zr.n > limit+8192 {
		t.Errorf("прочитано %d байт при пределе %d", zr.n, limit)
	}
	if got, serr := readBody(strings.NewReader(strings.Repeat("a", limit)), limit); serr != nil || len(got) != limit {
		t.Errorf("тело ровно в предел: %v", serr)
	}
	if maxFetchBody != 10<<20 {
		t.Errorf("предел тела: %d", maxFetchBody)
	}
}

func TestRouterParseMeta(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=1893456000")
	h.Set("Profile-Title", "base64:0JjQvNGP")
	h.Set("Profile-Update-Interval", "6")
	h.Set("Support-Url", "javascript:alert(1)")
	h.Set("Profile-Web-Page-Url", "https://cab.example/")
	h.Set("Announce", "base64:0JLQvdC40LzQsNC90LjQtQ==")
	h.Set("X-Hwid-Not-Supported", "true")
	m := parseMeta(h, now)
	if m.Upload != 1 || m.Download != 2 || m.Total != 3 || m.Expire != 1893456000 {
		t.Errorf("трафик и срок: %+v", m)
	}
	if m.Title != "Имя" || m.IntervalH != 6 || m.SupportURL != "" || m.WebURL != "https://cab.example/" || m.Announce != "Внимание" || !m.HWIDLocked || !m.UpdatedAt.Equal(now) {
		t.Errorf("метаданные: %+v", m)
	}

	h = http.Header{}
	h.Set("Profile-Title", "Name\x00\x1b[31m"+strings.Repeat("я", 300))
	h.Set("Support-Url", "data:text/html,x")
	h.Set("Profile-Web-Page-Url", "ftp://x.example/")
	h.Set("Profile-Update-Interval", "-3")
	m = parseMeta(h, now)
	if strings.ContainsAny(m.Title, "\x00\x1b") || len([]rune(m.Title)) > maxTitleRunes || m.SupportURL != "" || m.WebURL != "" || m.IntervalH != 0 || m.HWIDLocked {
		t.Errorf("недоверенные заголовки: %+v", m)
	}
	if m := parseMeta(nil, now); m.Title != "" || !m.UpdatedAt.Equal(now) {
		t.Errorf("без заголовков: %+v", m)
	}
}

func TestRouterExpiredWarning(t *testing.T) {
	expired := Meta{Expire: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Unix()}
	w := expiredWarning(expired, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if w == nil || w.Code != nodes.CodeExpired || w.Params["date"] != "2026-09-01" {
		t.Errorf("истекла подписка: %+v", w)
	}
	// Часы без NTP: «истекла» было бы ложным.
	if w := expiredWarning(expired, time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC)); w != nil {
		t.Errorf("при часах 1970 года предупреждение: %+v", w)
	}
	if w := expiredWarning(Meta{Expire: 1893456000}, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); w != nil {
		t.Errorf("срок не вышел, а предупреждение есть: %+v", w)
	}
	if w := expiredWarning(Meta{}, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); w != nil {
		t.Errorf("срока нет, а предупреждение есть: %+v", w)
	}
}

func TestRouterBlobStore(t *testing.T) {
	e := rtNewEnv(t, false, false)
	b := e.svc.blobs
	list := rtNodes(t, "vless_ws", "vless_reality", "trojan_ws")
	hash, err := b.put(list)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	// Порядок узлов в ответе провайдера на имя снимка не влияет.
	rev := []nodes.Node{list[2], list[1], list[0]}
	if h2, err := b.put(rev); err != nil || h2 != hash {
		t.Fatalf("put в другом порядке: %v %v", h2, err)
	}
	got, err := b.get(hash)
	if err != nil || len(got) != 3 {
		t.Fatalf("get: %v, узлов %d", err, len(got))
	}
	// Снимок после чтения даёт тот же узел и те же файлы ядер.
	for i, want := range list {
		var n nodes.Node
		for _, c := range got {
			if c.DupKey() == want.DupKey() {
				n = c
			}
		}
		if n.Host == "" {
			t.Fatalf("узел %d не найден в снимке", i)
		}
		wo, err1 := nodes.XrayOutbound(want, "xcp-t")
		go_, err2 := nodes.XrayOutbound(n, "xcp-t")
		wj, _ := json.Marshal(wo)
		gj, _ := json.Marshal(go_)
		if (err1 == nil) != (err2 == nil) || string(wj) != string(gj) {
			t.Errorf("узел %d: outbound после снимка изменился", i)
		}
	}
	path := filepath.Join(b.dir, hash+".json")
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("права снимка: %v %v", st, err)
	}

	// Испорченный снимок и пропавший снимок — ошибки, а не пустой набор.
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.get(hash); !errors.Is(err, errSnapshotCorrupt) {
		t.Errorf("испорченный снимок: %v", err)
	}
	if h3, err := b.put(list); err != nil || h3 != hash {
		t.Fatalf("put поверх испорченного: %v %v", h3, err)
	}
	if _, err := b.get(hash); err != nil {
		t.Errorf("снимок не восстановлен записью: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.get(hash); !errors.Is(err, errSnapshotMissing) {
		t.Errorf("пропавший снимок: %v", err)
	}
	for _, bad := range []string{"", "../x", strings.Repeat("g", 64), strings.Repeat("a", 63)} {
		if _, err := b.get(bad); err == nil {
			t.Errorf("имя снимка %q принято", bad)
		}
	}
}

// subSection — секция подписки для проверок генератора.
func rtSubSection(t *testing.T, id string, enabled bool) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(SubscriptionConfig{
		Kind: KindSubscription, ID: id, Name: "Подписка", URL: "https://provider.invalid/sub", Enabled: enabled,
		Exclude: []string{}, Interval: Interval{Mode: IntervalProvider},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rtLiveInput(t *testing.T, mode configlayer.SourceKind, sections map[string]json.RawMessage, ls LiveSources, cands Candidates) configlayer.Input {
	t.Helper()
	live := configlayer.Sections{}
	if err := writeLive(live, ls); err != nil {
		t.Fatal(err)
	}
	if err := writeCandidates(live, cands); err != nil {
		t.Fatal(err)
	}
	return configlayer.Input{Sections: configlayer.Sections(sections), Live: live, Mode: mode}
}

func rtPut(t *testing.T, e *rtEnv, names ...string) *Ref {
	t.Helper()
	list := rtNodes(t, names...)
	hash, err := e.svc.blobs.put(list)
	if err != nil {
		t.Fatal(err)
	}
	return &Ref{Hash: hash, FetchedAt: e.clock.Now(), Format: "links", Total: len(list), Usable: len(list)}
}

func rtSubXrayTags(t *testing.T, files []configlayer.GeneratedFile, id string) []string {
	t.Helper()
	if len(files) == 0 {
		return nil
	}
	if len(files) != 1 || files[0].RelPath != "04_outbounds.xcp-"+id+".tail.json" {
		t.Fatalf("файлы Xray: %+v", files)
	}
	var f struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(files[0].Content, &f); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, o := range f.Outbounds {
		tags = append(tags, o.Tag)
	}
	sort.Strings(tags)
	return tags
}

func TestRouterGenerateSubscriptionPerKernel(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const id = "a1b2c3d4"
	xrayRef := rtPut(t, e, "vless_ws", "vless_reality")
	mihomoRef := rtPut(t, e, "vless_ws", "tuic", "trojan_ws")
	oldXray := rtPut(t, e, "ss_plain")
	ls := LiveSources{id: {Kernels: map[string]*KernelLive{
		kernelXray:   {Pending: xrayRef, Applied: oldXray, Status: Status{Code: StatusOK}},
		kernelMihomo: {Pending: mihomoRef, Status: Status{Code: StatusOK}},
	}}}
	sections := map[string]json.RawMessage{sectionName(id): rtSubSection(t, id, true)}

	// Черновик: ожидающий снимок каждого ядра, наборы не дополняют друг друга.
	in := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
	xrayTags := rtSubXrayTags(t, rtGenerate(t, e.svc, kernelXray, in, rtBoth), id)
	wantTags := []string{}
	uniq, _ := nodes.Uniq(id, rtNodes(t, "vless_ws", "vless_reality"))
	for _, k := range uniq {
		wantTags = append(wantTags, nodes.XrayTag(id, k.Key))
	}
	sort.Strings(wantTags)
	if strings.Join(xrayTags, ",") != strings.Join(wantTags, ",") {
		t.Errorf("теги Xray: %v, нужны %v", xrayTags, wantTags)
	}
	mfiles := rtGenerate(t, e.svc, kernelMihomo, in, rtBoth)
	if len(mfiles) != 1 || mfiles[0].RelPath != "proxy_providers/xcp-"+id+".yaml" || !mfiles[0].Inert {
		t.Fatalf("файлы Mihomo: %+v", mfiles)
	}
	text := string(mfiles[0].Content)
	if !strings.Contains(text, "tuic") || strings.Contains(text, "reality") {
		t.Errorf("набор Mihomo должен быть из своего ответа (tuic есть, reality нет):\n%s", text)
	}

	// Применённое состояние без кандидата: применённый снимок; без него файла нет.
	in = rtLiveInput(t, configlayer.SourceApplied, sections, ls, nil)
	if got := rtSubXrayTags(t, rtGenerate(t, e.svc, kernelXray, in, rtBoth), id); len(got) != 1 {
		t.Errorf("теги применённого Xray: %v", got)
	}
	if files := rtGenerate(t, e.svc, kernelMihomo, in, rtBoth); len(files) != 0 {
		t.Errorf("у Mihomo нет применённого снимка, а файл есть: %+v", files)
	}
	// Кандидат оверлея перекрывает применённый снимок.
	in = rtLiveInput(t, configlayer.SourceApplied, sections, ls, Candidates{id: {kernelXray: *xrayRef}})
	if got := rtSubXrayTags(t, rtGenerate(t, e.svc, kernelXray, in, rtBoth), id); strings.Join(got, ",") != strings.Join(wantTags, ",") {
		t.Errorf("теги Xray с кандидатом: %v", got)
	}

	// Выключенный источник файла не даёт.
	in = rtLiveInput(t, configlayer.SourceDraft, map[string]json.RawMessage{sectionName(id): rtSubSection(t, id, false)}, ls, nil)
	if files := rtGenerate(t, e.svc, kernelXray, in, rtBoth); len(files) != 0 {
		t.Errorf("выключенный источник дал файл: %+v", files)
	}

	// Пропавший снимок — ошибка генератора, а не пустой набор.
	if err := os.Remove(filepath.Join(e.svc.blobs.dir, xrayRef.Hash+".json")); err != nil {
		t.Fatal(err)
	}
	e.svc.mmu.Lock()
	e.svc.gens = map[string]memoFile{}
	e.svc.genOrder = nil
	e.svc.mmu.Unlock()
	in = rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
	for _, g := range e.svc.Generators() {
		if g.ID() != xrayGenID {
			continue
		}
		files, err := g.Generate(in, rtBoth)
		if err == nil || len(files) != 0 || !errors.Is(err, errSnapshotMissing) {
			t.Errorf("пропавший снимок: файлы %d, ошибка %v", len(files), err)
		}
	}
}

func TestRouterCommitLive(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const id, orphan = "a1b2c3d4", "0f0f0f0f"
	oldRef := rtPut(t, e, "vless_ws", "trojan_ws")
	newRef := rtPut(t, e, "vless_ws", "vmess_ws_tls", "ss_plain")
	otherRef := rtPut(t, e, "tuic")
	sections := configlayer.Sections{sectionName(id): rtSubSection(t, id, true)}

	mk := func() LiveSources {
		return LiveSources{
			id: {Kernels: map[string]*KernelLive{
				kernelXray:   {Applied: oldRef, Pending: newRef, Status: Status{Code: StatusNetwork, ErrorCode: nodes.CodeTimeout}},
				kernelMihomo: {Pending: newRef, Status: Status{Code: StatusOK}},
			}},
			orphan: {Kernels: map[string]*KernelLive{kernelXray: {Applied: oldRef}}},
		}
	}
	state := func(ls LiveSources) *configlayer.State {
		st := &configlayer.State{Draft: sections.Clone(), Applied: configlayer.Sections{}, Live: configlayer.Sections{}}
		if err := writeLive(st.Live, ls); err != nil {
			t.Fatal(err)
		}
		if err := writeCandidates(st.Live, Candidates{"x": {}}); err != nil {
			t.Fatal(err)
		}
		return st
	}
	read := func(st *configlayer.State) LiveSources {
		ls, err := readLive(st.Live)
		if err != nil {
			t.Fatal(err)
		}
		return ls
	}

	// «Применить» из черновика: ожидающие снимки становятся применёнными.
	st := state(mk())
	used := configlayer.Input{Sections: sections, Live: state(mk()).Live, Mode: configlayer.SourceDraft}
	req := configlayer.ApplyRequest{Trigger: configlayer.TriggerUser, Source: configlayer.SourceDraft}
	if err := e.svc.CommitLive(st, req, used); err != nil {
		t.Fatalf("CommitLive: %v", err)
	}
	ls := read(st)
	x, m := ls[id].Kernels[kernelXray], ls[id].Kernels[kernelMihomo]
	if x.Applied.Hash != newRef.Hash || x.Pending != nil || x.Status.Code != StatusOK || x.Delta != (Delta{Added: 2, Removed: 1}) {
		t.Errorf("Xray после фиксации: applied %v pending %v статус %s разница %+v", x.Applied.Hash == newRef.Hash, x.Pending != nil, x.Status.Code, x.Delta)
	}
	if m.Applied == nil || m.Applied.Hash != newRef.Hash || m.Pending != nil || m.Delta != (Delta{Added: 3}) {
		t.Errorf("Mihomo после фиксации: %+v", m)
	}
	if _, ok := ls[orphan]; ok {
		t.Error("источник без секции остался в живых данных")
	}
	if _, ok := st.Live[LiveKeyCandidates]; ok {
		t.Error("ключ candidates сохранён в состоянии")
	}

	// Ожидающий снимок заменился после сборки файлов: фиксируется только собранный.
	changed := mk()
	changed[id].Kernels[kernelXray].Pending = otherRef
	st = state(changed)
	if err := e.svc.CommitLive(st, req, used); err != nil {
		t.Fatal(err)
	}
	x = read(st)[id].Kernels[kernelXray]
	if x.Applied.Hash != oldRef.Hash || x.Pending == nil || x.Pending.Hash != otherRef.Hash {
		t.Errorf("более свежий ожидающий снимок затронут: %+v", x)
	}

	// Запуск ограничен ядром: другое ядро не фиксируется.
	st = state(mk())
	if err := e.svc.CommitLive(st, configlayer.ApplyRequest{Trigger: configlayer.TriggerKernelInstalled, Source: configlayer.SourceDraft, Kernel: kernelMihomo}, used); err != nil {
		t.Fatal(err)
	}
	ls = read(st)
	if ls[id].Kernels[kernelXray].Pending == nil || ls[id].Kernels[kernelMihomo].Applied == nil {
		t.Errorf("запуск под одно ядро: %+v", ls[id])
	}

	// Фоновое обновление: кандидаты оверлея становятся применёнными.
	st = state(mk())
	overlay := configlayer.Sections{}
	if err := writeCandidates(overlay, Candidates{id: {kernelXray: *otherRef}}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CommitLive(st, configlayer.ApplyRequest{Trigger: configlayer.TriggerSourceRefresh, Source: configlayer.SourceApplied, LiveOverlay: overlay}, configlayer.Input{Mode: configlayer.SourceApplied}); err != nil {
		t.Fatal(err)
	}
	x = read(st)[id].Kernels[kernelXray]
	if x.Applied.Hash != otherRef.Hash || x.Pending != nil || x.Delta != (Delta{Added: 1, Removed: 2}) {
		t.Errorf("Xray после обновления: %+v", x)
	}
}
