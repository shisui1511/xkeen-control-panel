//go:build router

package sources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

// Диагностика ответа по ядрам: маскирование, предел, ленивый каталог, уборка. Узлы —
// настоящие ссылки testdata/links, разбор — настоящий разбор тела ответа.

func rtDiagDir(e *rtEnv) string { return filepath.Join(e.dir, "config_layer", "diag") }

// rtReadDiag читает файл диагностики как общий JSON (ключи snake_case) и как запись.
func rtReadDiag(t *testing.T, path string) (diagRecord, map[string]any, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл диагностики: %v", err)
	}
	var rec diagRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("диагностика не JSON: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	return rec, generic, data
}

func TestRouterDiagMaskedAndCapped(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const id = "a1b2c3d4"
	const token = "subtoken987654"
	subURL := "https://provider.invalid/api/" + token

	// Ответ: список ссылок (настоящие узлы), строка, которую разобрать нельзя, и
	// длинный хвост комментариев, чтобы тело превысило 2 МиБ.
	links := []string{rtLink(t, "vless_ws"), rtLink(t, "trojan_ws"), rtLink(t, "ss_plain"), "foo://broken", "# " + subURL}
	body := strings.Join(links, "\n") + "\n" + strings.Repeat("# "+strings.Repeat("x", 78)+"\n", (maxDiagBody/80)+1000)
	pr, err := nodes.ParseBody([]byte(body))
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if len(pr.Nodes) != 3 || len(pr.Skipped) != 1 {
		t.Fatalf("разбор ответа: узлов %d, пропусков %d", len(pr.Nodes), len(pr.Skipped))
	}
	fr := fetchResult{
		Body:   []byte(body),
		Status: 200,
		Header: http.Header{
			"Authorization":         {"Bearer topsecretvalue"},
			"Cookie":                {"session=abcdef123456"},
			"Set-Cookie":            {"sid=zzzzzzzzzzzz"},
			"Proxy-Authorization":   {"Basic dXNlcjpwYXNz"},
			"Profile-Web-Page-Url":  {subURL},
			"Subscription-Userinfo": {"upload=1; download=2; total=3"},
		},
		Secrets: urlSecrets(mustURL(t, subURL)),
	}
	e.svc.writeDiag(id, kernelXray, fr, pr)

	path := filepath.Join(rtDiagDir(e), id+"."+kernelXray+".json")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права файла диагностики: %v %v", st, err)
	}
	rec, generic, data := rtReadDiag(t, path)
	for _, k := range []string{"kernel", "fetched_at", "http_status", "headers", "body", "truncated", "report"} {
		if _, ok := generic[k]; !ok {
			t.Errorf("нет ключа %q", k)
		}
	}
	if rec.Kernel != kernelXray || rec.HTTPStatus != 200 || !rec.Truncated || len(rec.Body) > maxDiagBody {
		t.Errorf("запись: ядро %s, статус %d, усечено %v, тело %d байт", rec.Kernel, rec.HTTPStatus, rec.Truncated, len(rec.Body))
	}
	if rec.Report.Parsed != 3 || rec.Report.Stubs != 0 || len(rec.Report.Skipped) != 1 || rec.Report.Skipped[0].Code == "" {
		t.Errorf("отчёт: %+v", rec.Report)
	}
	sk := generic["report"].(map[string]any)["skipped"].([]any)[0].(map[string]any)
	for _, k := range []string{"line", "code", "fragment"} {
		if _, ok := sk[k]; !ok {
			t.Errorf("в пропуске нет ключа %q: %v", k, sk)
		}
	}

	for _, h := range rec.Headers {
		switch strings.ToLower(h.Name) {
		case "authorization", "cookie", "set-cookie", "proxy-authorization":
			if h.Value != "…" {
				t.Errorf("значение заголовка %s не скрыто: %q", h.Name, h.Value)
			}
		}
	}
	text := string(data)
	secrets := []string{token, "topsecretvalue", "abcdef123456", "zzzzzzzzzzzz", "dXNlcjpwYXNz", subURL}
	for _, n := range pr.Nodes {
		secrets = append(secrets, nodes.Secrets(n)...)
	}
	for _, s := range secrets {
		if len(s) >= 6 && strings.Contains(text, s) {
			t.Errorf("в диагностике остался секрет %q…", s[:4])
		}
	}
	if !strings.Contains(text, "Subscription-Userinfo") {
		t.Error("безопасный заголовок потерян")
	}
}

// Тело подписки в base64 хранится расшифрованным и замаскированным: учётные данные
// узлов не прячутся от маскирования внутри base64.
func TestRouterDiagBase64BodyMasked(t *testing.T) {
	e := rtNewEnv(t, false, false)
	raw := rtLink(t, "vless_ws") + "\n" + rtLink(t, "trojan_ws") + "\n"
	body := []byte(rtBase64(raw))
	pr, err := nodes.ParseBody(body)
	if err != nil || pr.Format != "base64" {
		t.Fatalf("ParseBody: %v, формат %s", err, pr.Format)
	}
	e.svc.writeDiag("0a0b0c0d", kernelMihomo, fetchResult{Body: body, Status: 200}, pr)
	rec, _, data := rtReadDiag(t, filepath.Join(rtDiagDir(e), "0a0b0c0d."+kernelMihomo+".json"))
	if strings.Contains(rec.Body, string(body[:32])) {
		t.Error("в записи осталось тело в base64")
	}
	for _, n := range pr.Nodes {
		for _, s := range nodes.Secrets(n) {
			if len(s) >= 6 && strings.Contains(string(data), s) {
				t.Errorf("секрет %q… в диагностике", s[:4])
			}
		}
	}
	if !strings.Contains(rec.Body, "vless://") {
		t.Errorf("расшифрованное тело не сохранено: %.40s", rec.Body)
	}
}

// Каталог diag создаётся при первой записи с правами 0700, а не в New.
func TestRouterDiagLazyDir(t *testing.T) {
	e := rtNewEnv(t, false, false)
	if _, err := os.Stat(rtDiagDir(e)); !os.IsNotExist(err) {
		t.Fatalf("каталог diag есть до первой записи: %v", err)
	}
	e.svc.writeDiag("a1b2c3d4", kernelXray, fetchResult{Status: 200}, nodes.ParseResult{})
	st, err := os.Stat(rtDiagDir(e))
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("каталог diag: %v %v", st, err)
	}
	// Идентификатор и ядро проверяются до построения пути.
	e.svc.writeDiag("../escape", kernelXray, fetchResult{}, nodes.ParseResult{})
	e.svc.writeDiag("a1b2c3d4", "other", fetchResult{}, nodes.ParseResult{})
	e.svc.writeDiag(MySourceID, kernelXray, fetchResult{}, nodes.ParseResult{})
	entries, _ := os.ReadDir(rtDiagDir(e))
	if len(entries) != 1 {
		t.Errorf("файлов в diag: %d, нужен один", len(entries))
	}
}

// Уборка удаляет диагностику источников, которых нет ни в черновике, ни в
// применённом состоянии, ни в живых токенах; остальную оставляет.
func TestRouterDiagGCUnknownSources(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const inDraft, orphan, tokenID, held = "a1b2c3d4", "0f0f0f0f", "11223344", "55667788"
	ref := rtPut(t, e, "vless_ws")
	rtAddSub(t, e, rtSubNamed(t, inDraft, "Провайдер", nil), inDraft, false, map[string]*Ref{kernelXray: ref})
	for _, id := range []string{inDraft, orphan, tokenID, held} {
		for _, k := range previewKernels {
			e.svc.writeDiag(id, k, fetchResult{Status: 200}, nodes.ParseResult{})
		}
	}
	cand := &subCandidate{kernels: map[string]*kernelOutcome{}, diagID: tokenID}
	if _, _, err := e.svc.putPreview(&previewEntry{subs: []*subCandidate{cand}}); err != nil {
		t.Fatal(err)
	}
	release := e.svc.blobs.holdDiag(held)
	stray := filepath.Join(rtDiagDir(e), "readme.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.svc.collectGarbage()
	list := func() string {
		entries, _ := os.ReadDir(rtDiagDir(e))
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	want := strings.Join([]string{
		inDraft + ".mihomo.json", inDraft + ".xray.json",
		held + ".mihomo.json", held + ".xray.json",
		"readme.txt",
		tokenID + ".mihomo.json", tokenID + ".xray.json",
	}, ",")
	sorted := strings.Split(want, ",")
	sort.Strings(sorted)
	if got := list(); got != strings.Join(sorted, ",") {
		t.Errorf("после уборки в diag: %s", got)
	}
	release()
	e.svc.collectGarbage()
	if got := list(); strings.Contains(got, held) {
		t.Errorf("диагностика отпущенного id осталась: %s", got)
	}
}

// Предпросмотр URL пишет диагностику каждого установленного ядра под идентификатором
// кандидата; при «Добавить» он становится идентификатором источника. Запрос идёт на
// настоящую сеть устройства: адрес loopback отклоняет защита от SSRF (ответа нет),
// диагностика пишется и при отказе.
func TestRouterPreviewWritesDiagPerKernel(t *testing.T) {
	for _, tc := range []struct {
		name         string
		xray, mihomo bool
		want         []string
	}{
		{"оба ядра", true, true, []string{kernelMihomo, kernelXray}},
		{"только Xray", true, false, []string{kernelXray}},
		{"только Mihomo", false, true, []string{kernelMihomo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := rtSubscriptionEnv(t, tc.xray, tc.mihomo, false)
			view, err := e.svc.Preview(context.Background(), inputOf("http://127.0.0.1:1/pathsecret1234"))
			if err != nil || view.Token == "" {
				t.Fatalf("Preview: %v, %+v", err, view)
			}
			entries, err := os.ReadDir(rtDiagDir(e))
			if err != nil {
				t.Fatalf("каталог diag: %v", err)
			}
			var kernels []string
			id := ""
			for _, en := range entries {
				m := diagFileRE.FindStringSubmatch(en.Name())
				if m == nil {
					t.Fatalf("лишний файл в diag: %s", en.Name())
				}
				if id != "" && id != m[1] {
					t.Fatalf("диагностика под двумя идентификаторами: %s и %s", id, m[1])
				}
				id = m[1]
				kernels = append(kernels, strings.TrimSuffix(strings.TrimPrefix(en.Name(), id+"."), ".json"))
				_, _, data := rtReadDiag(t, filepath.Join(rtDiagDir(e), en.Name()))
				if strings.Contains(string(data), "pathsecret1234") || strings.Contains(string(data), "127.0.0.1:1/") {
					t.Errorf("в диагностике адрес запроса: %s", en.Name())
				}
			}
			sort.Strings(kernels)
			if strings.Join(kernels, ",") != strings.Join(tc.want, ",") {
				t.Errorf("диагностика по ядрам: %v, нужно %v", kernels, tc.want)
			}

			// «Добавить»: идентификатор источника совпадает с идентификатором диагностики.
			if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
				t.Fatalf("Add: %v", err)
			}
			srcID, _, _ := rtDraftSubscription(t, e)
			if srcID != id {
				t.Errorf("идентификатор источника %s, диагностики %s", srcID, id)
			}
			for _, k := range tc.want {
				if !rtExists(filepath.Join(rtDiagDir(e), srcID+"."+k+".json")) {
					t.Errorf("после «Добавить» диагностика %s пропала", k)
				}
			}
		})
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func rtBase64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
