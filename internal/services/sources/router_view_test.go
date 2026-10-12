//go:build router

package sources

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Представление источников, правка через черновик, раскрытие URL и диагностика
// по ядру. Данные узлов — ссылки testdata/links, состояние — настоящий слой
// «Конфигурация» во временном каталоге теста; вердикт — ответы сервиса и секции
// черновика. Ядра не запускаются.

const rtViewSubURL = "https://sub.example/pathtoken-9f8e7d6c5b4a"

// rtViewEnv — окружение с обоими ядрами у сервиса и заданными активным и запущенным.
func rtViewEnv(t *testing.T, active string, running ...string) *rtEnv {
	t.Helper()
	e := rtNewEnvWith(t, false, false, func(d *Deps) {
		d.ActiveKernel = func() string { return active }
		d.Running = func(k string) bool {
			for _, r := range running {
				if r == k {
					return true
				}
			}
			return false
		}
	})
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	return e
}

// rtSubAt — секция подписки с URL, именем и временем добавления.
func rtSubAt(t *testing.T, id, name, subURL string, added time.Time) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(SubscriptionConfig{
		Kind: KindSubscription, ID: id, Name: name, URL: subURL, Enabled: true,
		Exclude: []string{}, Interval: Interval{Mode: IntervalProvider}, AddedAt: added,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rtView(t *testing.T, e *rtEnv) SourcesView {
	t.Helper()
	v, err := e.svc.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	return v
}

func rtFindSource(t *testing.T, v SourcesView, id string) SourceView {
	t.Helper()
	for _, s := range v.Sources {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("источника %s нет в представлении", id)
	return SourceView{}
}

func rtFindNode(t *testing.T, s SourceView, name string) NodeView {
	t.Helper()
	for _, n := range s.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("узла %q нет в источнике %s", name, s.ID)
	return NodeView{}
}

// rtSecretsOf собирает секреты всех узлов ссылок.
func rtSecretsOf(t *testing.T, names ...string) []string {
	t.Helper()
	var out []string
	for _, n := range rtNodes(t, names...) {
		out = append(out, nodes.Secrets(n)...)
	}
	if len(out) == 0 {
		t.Fatal("в ссылках нет секретов для проверки маскирования")
	}
	return out
}

// Подписка с разными ответами ядер и «Мои узлы»: счётчики по ядрам, объединённый
// список с in_set и вердиктами, статус, снимок; в ответе нет секретов.
func TestRouterViewTracer(t *testing.T) {
	e := rtViewEnv(t, "xray", kernelXray)
	const id = "a1b2c3d4"
	names := []string{"vless_ws", "trojan_grpc"}
	xrayRef := rtPut(t, e, names...)
	mihomoRef := rtPut(t, e, "vless_ws", "trojan_grpc", "tuic")
	mihomoRef.Stubs = 1
	rtAddSub(t, e, rtSubAt(t, id, "Провайдер", rtViewSubURL, e.clock.Now()), id, true,
		map[string]*Ref{kernelXray: xrayRef, kernelMihomo: mihomoRef})
	e.addLinks(t, rtLink(t, "vless_reality"))

	v := rtView(t, e)
	if v.DefaultKernel != kernelXray {
		t.Errorf("default_kernel %q, ожидался xray", v.DefaultKernel)
	}
	if !v.Installed.Xray || !v.Installed.Mihomo || !v.Running.Xray || v.Running.Mihomo {
		t.Errorf("installed %+v, running %+v", v.Installed, v.Running)
	}
	if len(v.Sources) != 2 || v.Sources[0].ID != id || v.Sources[1].ID != MySourceID {
		t.Fatalf("порядок источников: %+v", v.Sources)
	}
	sub := v.Sources[0]
	if sub.Kernels[kernelXray].Count != 2 {
		t.Errorf("у Xray %d узлов, ожидалось 2", sub.Kernels[kernelXray].Count)
	}
	if sub.Kernels[kernelMihomo].Count != 3 {
		t.Errorf("у Mihomo %d узлов, ожидалось 3", sub.Kernels[kernelMihomo].Count)
	}
	if sub.Kernels[kernelXray].Status.Code != StatusOK || sub.Kernels[kernelXray].SnapshotAt == nil {
		t.Errorf("статус Xray: %+v", sub.Kernels[kernelXray])
	}
	if !sub.Kernels[kernelMihomo].Stopped || sub.Kernels[kernelXray].Stopped {
		t.Errorf("stopped: xray %v, mihomo %v", sub.Kernels[kernelXray].Stopped, sub.Kernels[kernelMihomo].Stopped)
	}
	if sub.Stubs != 1 || len(sub.Nodes) != 3 {
		t.Errorf("stubs %d, узлов %d (ожидалось 1 и 3)", sub.Stubs, len(sub.Nodes))
	}
	var onlyMihomo, both int
	for _, n := range sub.Nodes {
		switch {
		case n.InSet.Xray && n.InSet.Mihomo:
			both++
		case !n.InSet.Xray && n.InSet.Mihomo:
			onlyMihomo++
			if n.Support.Xray.OK || n.Support.Xray.Code == "" {
				t.Errorf("узел только из ответа Mihomo: вердикт Xray %+v должен быть отказом с кодом", n.Support.Xray)
			}
			if !n.Support.Mihomo.OK {
				t.Errorf("tuic Mihomo умеет: %+v", n.Support.Mihomo)
			}
		}
		if n.Ref == "" || n.Key == "" || n.CredMask == "" {
			t.Errorf("узел без ref, key или маски: %+v", n)
		}
	}
	if both != 2 || onlyMihomo != 1 {
		t.Errorf("общих узлов %d, только Mihomo %d (ожидалось 2 и 1)", both, onlyMihomo)
	}
	if sub.DraftState != DraftAdded {
		t.Errorf("подписка вне применённого состояния: draft_state %q, ожидался added", sub.DraftState)
	}
	my := v.Sources[1]
	if len(my.Nodes) != 1 || my.Nodes[0].EntryID == "" || my.Kernels[kernelXray].Count != 1 {
		t.Errorf("«Мои узлы»: %+v", my)
	}

	// Ни URL подписки, ни учётных данных узлов в представлении нет.
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, rtViewSubURL) || strings.Contains(text, "pathtoken-9f8e7d6c5b4a") {
		t.Error("в представлении полный URL подписки или его токен")
	}
	if want := "https://sub.example/…/path…"; !strings.Contains(text, want) {
		t.Errorf("нет маски URL %q", want)
	}
	for _, sec := range rtSecretsOf(t, "vless_ws", "trojan_grpc", "tuic", "vless_reality") {
		if len(sec) >= 6 && strings.Contains(text, sec) {
			t.Errorf("в представлении учётные данные узла %.4s…", sec)
		}
	}
	for _, key := range []string{`"default_kernel"`, `"in_set"`, `"draft_state"`, `"url_masked"`, `"cred_mask"`} {
		if !strings.Contains(text, key) {
			t.Errorf("в JSON нет ключа %s", key)
		}
	}
}

// Подписки идут по дате добавления (новые сверху), «Мои узлы» — последними;
// пустое состояние — пустой список.
func TestRouterViewOrderAndEmpty(t *testing.T) {
	e := rtViewEnv(t, "")
	if v := rtView(t, e); v.Sources == nil || len(v.Sources) != 0 {
		t.Fatalf("пустой слой: %+v", v.Sources)
	}
	t1 := e.clock.Now()
	ids := []string{"00000001", "00000002", "00000003"}
	for i, id := range ids {
		section := rtSubAt(t, id, "Источник "+id, rtViewSubURL+id, t1.Add(time.Duration(i)*time.Hour))
		rtAddSub(t, e, section, id, true, map[string]*Ref{kernelXray: rtPut(t, e, "vless_ws")})
	}
	e.addLinks(t, rtLink(t, "vless_ws"))
	v := rtView(t, e)
	var got []string
	for _, s := range v.Sources {
		got = append(got, s.ID)
	}
	if want := "00000003,00000002,00000001,my"; strings.Join(got, ",") != want {
		t.Errorf("порядок %v, ожидался %s", got, want)
	}
}

// Ядро просмотра по умолчанию: активное, иначе первое установленное.
func TestRouterViewDefaultKernel(t *testing.T) {
	cases := []struct {
		active    string
		installed configlayer.InstalledKernels
		want      string
	}{
		{"", configlayer.InstalledKernels{Mihomo: true}, kernelMihomo},
		{"none", configlayer.InstalledKernels{Xray: true, Mihomo: true}, kernelXray},
		{"mihomo", configlayer.InstalledKernels{Xray: true, Mihomo: true}, kernelMihomo},
		{"mihomo", configlayer.InstalledKernels{Xray: true}, kernelXray},
		{"", configlayer.InstalledKernels{}, ""},
	}
	for _, tc := range cases {
		e := rtViewEnv(t, tc.active)
		e.installed = tc.installed
		if got := rtView(t, e).DefaultKernel; got != tc.want {
			t.Errorf("active %q, установлены %+v: default_kernel %q, ожидалось %q", tc.active, tc.installed, got, tc.want)
		}
	}
}

// Правка настроек подписки идёт в черновик с ревизией и проверкой значений.
func TestRouterManageUpdate(t *testing.T) {
	e := rtViewEnv(t, "xray", kernelXray)
	const id = "a1b2c3d4"
	rtAddSub(t, e, rtSubAt(t, id, "Провайдер", rtViewSubURL, e.clock.Now()), id, false,
		map[string]*Ref{kernelXray: rtPut(t, e, "vless_ws")})

	section := func() SubscriptionConfig {
		st, err := e.layer.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := parseSubscription(st.Draft[sectionName(id)])
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }

	rev := e.rev(t)
	ev, err := e.svc.Update(rev, id, Patch{Name: str("Мой провайдер")})
	if err != nil {
		t.Fatalf("Update name: %v", err)
	}
	if ev.DraftRevision != rev+1 || section().Name != "Мой провайдер" {
		t.Errorf("имя: ревизия %d (была %d), секция %+v", ev.DraftRevision, rev, section())
	}
	if _, err := e.svc.Update(e.rev(t), id, Patch{Enabled: flag(false)}); err != nil || section().Enabled {
		t.Errorf("Enabled false: err %v, секция %+v", err, section())
	}
	excl := []string{"ru", " ", "test"}
	if _, err := e.svc.Update(e.rev(t), id, Patch{Exclude: &excl}); err != nil || strings.Join(section().Exclude, ",") != "ru,test" {
		t.Errorf("Exclude: err %v, %v", err, section().Exclude)
	}
	for _, c := range []struct {
		iv Interval
		ok bool
	}{
		{Interval{Mode: IntervalHours, Hours: 6}, true},
		{Interval{Mode: IntervalHours, Hours: 5}, false},
		{Interval{Mode: IntervalOff}, true},
		{Interval{Mode: "weekly"}, false},
	} {
		iv := c.iv
		_, err := e.svc.Update(e.rev(t), id, Patch{Interval: &iv})
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrInvalidInput)) {
			t.Errorf("интервал %+v: err %v, допустимым должен быть %v", c.iv, err, c.ok)
		}
	}
	if section().Interval.Mode != IntervalOff {
		t.Errorf("интервал после проверок: %+v", section().Interval)
	}
	if _, err := e.svc.Update(e.rev(t), id, Patch{HWID: str("ABC123")}); err != nil {
		t.Fatalf("HWID: %v", err)
	}
	v := rtView(t, e)
	sub := rtFindSource(t, v, id)
	if sub.Settings == nil || !sub.Settings.HWIDCustom {
		t.Errorf("hwid_custom не выставлен: %+v", sub.Settings)
	}
	if data, _ := json.Marshal(v); strings.Contains(string(data), "ABC123") {
		t.Error("HWID источника попал в представление")
	}

	// Смена URL: ID прежний, ожидающий снимок снят, применённого у подписки нет.
	if _, err := e.svc.Update(e.rev(t), id, Patch{URL: str("https://new.example/s")}); err != nil {
		t.Fatalf("URL: %v", err)
	}
	if section().URL != "https://new.example/s" {
		t.Errorf("URL не заменён: %q", section().URL)
	}
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	live, err := readLive(st.Live)
	if err != nil {
		t.Fatal(err)
	}
	if kl := live[id].Kernels[kernelXray]; kl == nil || kl.Pending != nil {
		t.Errorf("ожидающий снимок после смены URL: %+v", kl)
	}
	// Метаданные прежнего адреса (в том числе «без HWID») сняты: срок обновления
	// наступил, новый адрес запрашивается на ближайшем тике.
	if live[id].Meta != (Meta{}) {
		t.Errorf("метаданные после смены URL: %+v", live[id].Meta)
	}

	// Проверка значений.
	long := strings.Repeat("я", 129)
	many := make([]string, 101)
	for i := range many {
		many[i] = "x" + strings.Repeat("a", i%5) + string(rune('A'+i%26)) + string(rune('a'+i/26))
	}
	bad := []Patch{
		{Name: &long},
		{Name: str("имя\x00с нулём")},
		{Exclude: &many},
		{HWID: str("ab cd")},
		{HWID: str(strings.Repeat("a", 129))},
		{URL: str("ftp://x.example/s")},
		{URL: str("не ссылка")},
	}
	for i, p := range bad {
		if _, err := e.svc.Update(e.rev(t), id, p); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("недопустимая правка %d: err %v, ожидался ErrInvalidInput", i, err)
		}
	}
	if _, err := e.svc.Update(e.rev(t)-1, id, Patch{Name: str("x")}); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия: %v", err)
	}
	if _, err := e.svc.Update(e.rev(t), "deadbeef", Patch{Name: str("x")}); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("неизвестный источник: %v", err)
	}
}

// «Мои узлы» меняют только включение; ссылка и прочие настройки отклоняются.
func TestRouterManageUpdateMySource(t *testing.T) {
	e := rtViewEnv(t, "")
	e.addLinks(t, rtLink(t, "vless_ws"))
	off, u := false, "https://x.example/s"
	if _, err := e.svc.Update(e.rev(t), MySourceID, Patch{Enabled: &off}); err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if e.my(t).Enabled {
		t.Error("«Мои узлы» остались включёнными")
	}
	if _, err := e.svc.Update(e.rev(t), MySourceID, Patch{URL: &u}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("URL у «Моих узлов»: %v", err)
	}
	if _, err := e.svc.Delete(e.rev(t), MySourceID); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("удаление «Моих узлов»: %v", err)
	}
}

// Удаление убирает секцию из черновика и живые данные подписки, которой нет в
// применённом состоянии; раскрытие URL — только по Reveal и без записи в журнал.
func TestRouterManageDeleteAndReveal(t *testing.T) {
	e := rtViewEnv(t, "")
	const id = "a1b2c3d4"
	rtAddSub(t, e, rtSubAt(t, id, "Провайдер", rtViewSubURL, e.clock.Now()), id, false,
		map[string]*Ref{kernelXray: rtPut(t, e, "vless_ws")})

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	got, err := e.svc.Reveal(id)
	if err != nil || got != rtViewSubURL {
		t.Fatalf("Reveal: %q, %v", got, err)
	}
	if _, err := e.svc.Reveal("deadbeef"); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("Reveal неизвестного: %v", err)
	}
	if _, err := e.svc.Reveal(MySourceID); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("Reveal «Моих узлов»: %v", err)
	}
	if strings.Contains(logs.String(), "pathtoken") {
		t.Errorf("Reveal записал URL в журнал: %s", logs.String())
	}

	rev := e.rev(t)
	if _, err := e.svc.Delete(rev-1, id); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия: %v", err)
	}
	if _, err := e.svc.Delete(rev, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Draft[sectionName(id)]; ok {
		t.Error("секция осталась в черновике")
	}
	if live, _ := readLive(st.Live); live[id] != nil {
		t.Error("живые данные неприменённой подписки остались")
	}
	for _, s := range rtView(t, e).Sources {
		if s.ID == id {
			t.Error("удалённая подписка осталась в представлении")
		}
	}
	if _, err := e.svc.Delete(e.rev(t), id); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
}

// Дубли «Моих узлов» с подпиской и состояние ядра: остановленное, ожидающий
// снимок, разница, повтор после сети.
func TestRouterViewDupAndKernelState(t *testing.T) {
	e := rtViewEnv(t, "xray", kernelXray)
	const id = "a1b2c3d4"
	rtAddSub(t, e, rtSubAt(t, id, "Провайдер", rtViewSubURL, e.clock.Now()), id, false,
		map[string]*Ref{kernelXray: rtPut(t, e, "vless_ws", "trojan_grpc")})
	e.addLinks(t, rtLink(t, "vless_ws"))

	my := rtFindSource(t, rtView(t, e), MySourceID)
	if len(my.Nodes) != 1 || strings.Join(my.Nodes[0].DupIn, ",") != "Провайдер" {
		t.Errorf("dup_in: %+v", my.Nodes)
	}
	sub := rtFindSource(t, rtView(t, e), id)
	if ks := sub.Kernels[kernelXray]; !ks.Pending || ks.Stopped {
		t.Errorf("ядро Xray: pending %v, stopped %v (ожидалось true, false)", ks.Pending, ks.Stopped)
	}
	if ks, ok := sub.Kernels[kernelMihomo]; !ok || !ks.Stopped || ks.Status.Code != StatusNone {
		t.Errorf("ядро Mihomo без запроса: %+v", ks)
	}

	retry := e.clock.Now().Add(5 * time.Minute)
	if err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		kl := cur[id].kernelLive(kernelMihomo)
		kl.Status = Status{Code: StatusNetwork, ErrorCode: "dns", Since: e.clock.Now(), RetryAt: retry, Attempts: 2}
		cur[id].kernelLive(kernelXray).Delta = Delta{Added: 1}
		return writeLive(l, cur)
	}); err != nil {
		t.Fatal(err)
	}
	sub = rtFindSource(t, rtView(t, e), id)
	ks := sub.Kernels[kernelMihomo]
	if ks.Status.Code != StatusNetwork || ks.Status.Attempts != 2 || ks.Status.RetryAt == nil || !ks.Status.RetryAt.Equal(retry) {
		t.Errorf("статус Mihomo: %+v", ks.Status)
	}
	if d := sub.Kernels[kernelXray].Delta; d == nil || d.Added != 1 || d.Removed != 0 {
		t.Errorf("delta Xray: %+v", d)
	}
}

// Диагностика по ядру: записанный при запросе файл отдаётся замаскированным;
// без запроса — empty; чужое ядро и неизвестный источник — ошибки.
func TestRouterDiagnosticsView(t *testing.T) {
	e := rtViewEnv(t, "")
	const id = "a1b2c3d4"
	rtAddSub(t, e, rtSubAt(t, id, "Провайдер", rtViewSubURL, e.clock.Now()), id, false,
		map[string]*Ref{kernelXray: rtPut(t, e, "vless_ws")})

	body := rtLink(t, "vless_ws") + "\n" + rtLink(t, "tuic") + "\n"
	pr, err := nodes.ParseBody([]byte(body))
	if err != nil {
		t.Fatalf("разбор тела: %v", err)
	}
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer very-secret-value")
	hdr.Set("Set-Cookie", "sid=cookie-secret-value")
	hdr.Set("Content-Type", "text/plain")
	e.svc.writeDiag(id, kernelXray, fetchResult{Body: []byte(body), Header: hdr, Status: 200, Secrets: []string{"pathtoken-9f8e7d6c5b4a"}}, pr)

	v, err := e.svc.Diagnostics(id, kernelXray)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if v.Empty || v.Kernel != kernelXray || v.HTTPStatus != 200 || v.FetchedAt == nil || v.Report.Parsed != len(pr.Nodes) {
		t.Fatalf("диагностика: %+v", v)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range append(rtSecretsOf(t, "vless_ws", "tuic"), "very-secret-value", "cookie-secret-value") {
		if len(sec) >= 6 && strings.Contains(string(data), sec) {
			t.Errorf("в диагностике секрет %.4s…", sec)
		}
	}
	if len(v.Headers) == 0 || v.Report.Skipped == nil {
		t.Errorf("headers %v, skipped %v", v.Headers, v.Report.Skipped)
	}

	if m, err := e.svc.Diagnostics(id, kernelMihomo); err != nil || !m.Empty || m.Kernel != kernelMihomo {
		t.Errorf("Mihomo без запроса: %+v, %v", m, err)
	}
	if _, err := e.svc.Diagnostics(id, "sing"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("чужое ядро: %v", err)
	}
	if _, err := e.svc.Diagnostics("deadbeef", kernelXray); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("неизвестный источник: %v", err)
	}
}
