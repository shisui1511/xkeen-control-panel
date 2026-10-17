//go:build router

package nodes

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка распознавателя ввода на устройстве. Вердикт — свойства результата
// (URL, узлы, ошибки строк с кодами и номерами) на данных share-ссылок и входных
// тел из testdata; эталонных файлов нет. Сеть распознаватель не трогает.

// rtLink — первая ссылка файла данных схемы.
func rtLink(t *testing.T, scheme string) string {
	t.Helper()
	return rtFirstLink(t, filepath.Join(routertest.LinksDir(t), scheme+".txt"))
}

// rtAllLinks — все ссылки из каталога данных в порядке имён файлов.
func rtAllLinks(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(routertest.LinksDir(t), "*.txt"))
	sort.Strings(files)
	var out []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// rtErrorAt — ошибка с заданной строкой и кодом.
func rtErrorAt(rec Recognition, file string, line int, code string) *LineError {
	for i := range rec.Errors {
		e := &rec.Errors[i]
		if e.File == file && e.Line == line && e.Err != nil && e.Err.Code == code {
			return e
		}
	}
	return nil
}

// rtCheckFragments — инварианты D-33 для всех ошибок: фрагмент не длиннее 80
// символов, без userinfo и запроса и без перечисленных секретов; у каждой
// ошибки есть код.
func rtCheckFragments(t *testing.T, rec Recognition, secrets ...string) {
	t.Helper()
	for _, e := range rec.Errors {
		if e.Err == nil || e.Err.Code == "" {
			t.Errorf("ошибка без кода: %+v", e)
			continue
		}
		if utf8.RuneCountInString(e.Fragment) > MaxFragment {
			t.Errorf("фрагмент длиннее %d символов (строка %d, %s)", MaxFragment, e.Line, e.Err.Code)
		}
		if e.Err.Fragment != e.Fragment || e.Err.Line != e.Line {
			t.Errorf("Err и LineError расходятся: %+v / %+v", e, e.Err)
		}
		if e.Err.Detail != "" {
			t.Errorf("Detail не пуст (строка %d, %s)", e.Line, e.Err.Code)
		}
		if strings.ContainsAny(e.Fragment, "@?") && !strings.Contains(e.Fragment, " ") {
			t.Errorf("во фрагменте userinfo или запрос (строка %d, %s)", e.Line, e.Err.Code)
		}
		for _, s := range secrets {
			if s != "" && (strings.Contains(e.Fragment, s) || strings.Contains(e.Err.Error(), s)) {
				t.Errorf("секрет в ошибке строки %d (%s)", e.Line, e.Err.Code)
			}
		}
	}
}

func TestRouterRecognizeTracer(t *testing.T) {
	routertest.Current(t)

	vless := rtLink(t, "vless_ws")
	text := "https://sub.example/api/sub/tok\n" + vless + "\nпривет мир\nfoo://u@h.example:1\n"
	rec := Recognize(Input{Text: text})

	if len(rec.URLs) != 1 || rec.URLs[0].Line != 1 || rec.URLs[0].URL != "https://sub.example/api/sub/tok" || rec.URLs[0].File != "" {
		t.Fatalf("URL: %+v", rec.URLs)
	}
	if len(rec.Nodes) != 1 || rec.Nodes[0].Text != vless || rec.Nodes[0].Format != "link" || rec.Nodes[0].Protocol != "vless" {
		t.Fatalf("узлы: %d, первый %+v", len(rec.Nodes), rec.Nodes)
	}
	if len(rec.Errors) != 2 || rec.NodesOnly || rec.Collapsed != 0 {
		t.Fatalf("ошибок %d, nodesOnly %v, collapsed %d: %+v", len(rec.Errors), rec.NodesOnly, rec.Collapsed, rec.Errors)
	}
	if e := rtErrorAt(rec, "", 3, CodeUnrecognized); e == nil || e.Fragment != "привет мир" {
		t.Errorf("строка 3: %+v", e)
	}
	e4 := rtErrorAt(rec, "", 4, CodeUnknownScheme)
	if e4 == nil || e4.Err.Params["scheme"] != "foo" || e4.Fragment != "foo://h.example:1" {
		t.Errorf("строка 4: %+v", e4)
	}
	rtCheckFragments(t, rec, "u@")
}

func TestRouterRecognizeEmptyAndInvalid(t *testing.T) {
	routertest.Current(t)

	rec := Recognize(Input{})
	if len(rec.URLs)+len(rec.Nodes)+len(rec.Errors) != 0 || rec.NodesOnly || rec.Collapsed != 0 {
		t.Errorf("пустой ввод: %+v", rec)
	}
	rec = Recognize(Input{Text: "\n  \n# комментарий\n\t# ещё\n"})
	if len(rec.URLs)+len(rec.Nodes)+len(rec.Errors) != 0 {
		t.Errorf("комментарии: %+v", rec)
	}

	rec = Recognize(Input{Text: "https://\nftp://x.example/a\nhttps:///path\n"})
	if len(rec.URLs) != 0 || len(rec.Nodes) != 0 {
		t.Fatalf("недопустимые URL стали элементами: %+v", rec)
	}
	if rtErrorAt(rec, "", 1, CodeInvalidURL) == nil || rtErrorAt(rec, "", 3, CodeInvalidURL) == nil {
		t.Errorf("invalid_url: %+v", rec.Errors)
	}
	if e := rtErrorAt(rec, "", 2, CodeUnknownScheme); e == nil || e.Err.Params["scheme"] != "ftp" || e.Fragment != "ftp://x.example" {
		t.Errorf("ftp: %+v", e)
	}
	rtCheckFragments(t, rec)
}

func TestRouterRecognizeStructured(t *testing.T) {
	routertest.Current(t)

	// Полный конфиг Xray: только узлы и признак NodesOnly.
	rec := Recognize(Input{Text: string(rtInput(t, "xray_full.json"))})
	if len(rec.Nodes) != 1 || !rec.NodesOnly || len(rec.Errors) != 0 || rec.Nodes[0].Format != "json" {
		t.Errorf("xray_full: узлов %d, nodesOnly %v, ошибки %+v", len(rec.Nodes), rec.NodesOnly, rec.Errors)
	}
	// Xray: только outbounds — не полный конфиг.
	rec = Recognize(Input{Text: string(rtInput(t, "xray_outbounds.json"))})
	if len(rec.Nodes) != 2 || rec.NodesOnly {
		t.Errorf("xray_outbounds: узлов %d, nodesOnly %v, ошибки %+v", len(rec.Nodes), rec.NodesOnly, rec.Errors)
	}
	// Массив outbound sing-box.
	singbox := `[{"type":"trojan","tag":"s1","server":"node1.example","server_port":443,"password":"pw-secret-1","tls":{"enabled":true,"server_name":"cdn.example"}}]`
	rec = Recognize(Input{Text: singbox})
	if len(rec.Nodes) != 1 || rec.NodesOnly || rec.Nodes[0].Origin != OriginSingbox || rec.Nodes[0].Format != "json" {
		t.Errorf("sing-box: %+v ошибки %+v", rec.Nodes, rec.Errors)
	}
	// Ошибка JSON: код, строка и имя файла.
	rec = Recognize(Input{Files: []File{{Name: "dir/bad.json", Content: "{\n  \"outbounds\": [\n    ,\n  ]\n}"}}})
	if e := rtErrorAt(rec, "bad.json", 3, CodeJSONError); e == nil {
		t.Errorf("json_error: %+v", rec.Errors)
	}

	// YAML: proxies с rules — только узлы.
	yamlFull := "proxies:\n  - name: a\n    type: trojan\n    server: node1.example\n    port: 443\n    password: pw-secret-2\nproxy-groups:\n  - name: g\n    type: select\n    proxies: [a]\nrules:\n  - MATCH,g\n"
	rec = Recognize(Input{Text: yamlFull})
	if len(rec.Nodes) != 1 || !rec.NodesOnly || rec.Nodes[0].Format != "yaml" {
		t.Errorf("yaml с rules: узлов %d, nodesOnly %v, ошибки %+v", len(rec.Nodes), rec.NodesOnly, rec.Errors)
	}
	rec = Recognize(Input{Text: string(rtInput(t, "clash_anchors.yaml"))})
	if len(rec.Nodes) != 2 || rec.NodesOnly || len(rec.Errors) != 0 {
		t.Errorf("clash_anchors: узлов %d, nodesOnly %v, ошибки %+v", len(rec.Nodes), rec.NodesOnly, rec.Errors)
	}
	// YAML-список без ключа proxies.
	list := "- name: a\n  type: trojan\n  server: node1.example\n  port: 443\n  password: pw-secret-3\n- name: b\n  type: trojan\n  server: node2.example\n  port: 443\n  password: pw-secret-4\n"
	rec = Recognize(Input{Text: list})
	if len(rec.Nodes) != 2 || rec.NodesOnly || len(rec.Errors) != 0 {
		t.Errorf("список «- name:»: узлов %d, ошибки %+v", len(rec.Nodes), rec.Errors)
	}

	// Два блока [Interface] подряд.
	rec = Recognize(Input{Text: string(rtInput(t, "wg_multi.conf"))})
	if len(rec.Nodes) != 2 || len(rec.Errors) != 0 || rec.Nodes[0].Protocol != "wireguard" || rec.Nodes[0].Format != "wg" {
		t.Errorf("wg_multi: узлов %d, ошибки %+v", len(rec.Nodes), rec.Errors)
	}
	// Один блок.
	rec = Recognize(Input{Text: string(rtInput(t, "wg_classic.conf"))})
	if len(rec.Nodes) != 1 || len(rec.Errors) != 0 {
		t.Errorf("wg_classic: узлов %d, ошибки %+v", len(rec.Nodes), rec.Errors)
	}

	// base64-блок ссылок.
	b64 := strings.TrimSpace(string(rtInput(t, "real_links.b64")))
	rec = Recognize(Input{Text: b64})
	if len(rec.Nodes) == 0 || len(rec.URLs) != 0 {
		t.Errorf("base64-блок: узлов %d, URL %d, ошибки %+v", len(rec.Nodes), len(rec.URLs), rec.Errors)
	}
	// То же с переносами по ширине.
	var wrapped []string
	for i := 0; i < len(b64); i += 40 {
		wrapped = append(wrapped, b64[i:min(i+40, len(b64))])
	}
	rec2 := Recognize(Input{Text: strings.Join(wrapped, "\n")})
	if len(rec2.Nodes) != len(rec.Nodes) {
		t.Errorf("base64 с переносами: узлов %d, ожидалось %d, ошибки %+v", len(rec2.Nodes), len(rec.Nodes), rec2.Errors)
	}

	// base64 без «://» после декодирования — unrecognized, а не bad_base64.
	plain := "aGVsbG8gd29ybGQsIGhlbGxvIHdvcmxkLCBoZWxsbyB3b3JsZA=="
	rec = Recognize(Input{Text: plain})
	if len(rec.Errors) != 1 || rtErrorAt(rec, "", 1, CodeUnrecognized) == nil {
		t.Errorf("base64 без ссылок: %+v", rec.Errors)
	}
	// Похожий на подписку блок с битым заполнением — bad_base64.
	rec = Recognize(Input{Text: "AAAAAAAAAAAAAAAAA"})
	if len(rec.Errors) != 1 || rtErrorAt(rec, "", 1, CodeBadBase64) == nil {
		t.Errorf("битый base64: %+v", rec.Errors)
	}
	rtCheckFragments(t, rec, "AAAAAAAAAAAAAAAAA")
}

func TestRouterRecognizeMixedWithWG(t *testing.T) {
	routertest.Current(t)

	vless, trojan := rtLink(t, "vless_ws"), rtLink(t, "trojan_grpc")
	wg := strings.TrimSpace(string(rtInput(t, "wg_classic.conf")))
	text := vless + "\n" + trojan + "\n\n" + wg + "\n\nhttps://sub.example/p/xyz\n"
	rec := Recognize(Input{Text: text})
	if len(rec.Nodes) != 3 || len(rec.URLs) != 1 || len(rec.Errors) != 0 {
		t.Fatalf("узлов %d, URL %d, ошибки %+v", len(rec.Nodes), len(rec.URLs), rec.Errors)
	}
	wantLine := strings.Count(text, "\n") // URL в последней строке текста
	if rec.URLs[0].Line != wantLine {
		t.Errorf("строка URL: %d, ожидалась %d", rec.URLs[0].Line, wantLine)
	}
	if rec.Nodes[2].Protocol != "wireguard" || rec.Nodes[2].Text != wg {
		t.Errorf("wg-узел: %s, Text совпадает: %v", rec.Nodes[2].Protocol, rec.Nodes[2].Text == wg)
	}

	// Ошибка внутри wg-блока: строка абсолютная.
	bad := strings.TrimSpace(string(rtInput(t, "wg_missing_private.conf")))
	text = vless + "\n\n" + bad + "\n\nпривет\n"
	rec = Recognize(Input{Text: text})
	if len(rec.Nodes) != 1 {
		t.Errorf("узлов %d: %+v", len(rec.Nodes), rec.Errors)
	}
	if e := rtErrorAt(rec, "", 3, CodeWGMissingKey); e == nil || e.Err.Params["key"] != "PrivateKey" {
		t.Errorf("wg_missing_key на строке 3: %+v", rec.Errors)
	}
	badLines := strings.Count(bad, "\n") + 1
	if rtErrorAt(rec, "", 3+badLines+1, CodeUnrecognized) == nil {
		t.Errorf("строка после блока не разобрана отдельно: %+v", rec.Errors)
	}
	rtCheckFragments(t, rec)
}

func TestRouterRecognizeFiles(t *testing.T) {
	routertest.Current(t)

	vless := rtLink(t, "vless_ws")
	rec := Recognize(Input{
		Text: "https://sub.example/api/s1\n",
		Files: []File{
			{Name: "a.yaml", Content: string(rtInput(t, "clash_anchors.yaml"))},
			{Name: "b.conf", Content: string(rtInput(t, "wg_classic.conf"))},
			{Name: "c.txt", Content: vless + "\nhttps://sub.example/api/s2\n"},
		},
	})
	if len(rec.Nodes) != 4 || len(rec.URLs) != 2 || len(rec.Errors) != 0 {
		t.Fatalf("узлов %d, URL %d, ошибки %+v", len(rec.Nodes), len(rec.URLs), rec.Errors)
	}
	if rec.URLs[0].File != "" || rec.URLs[1].File != "c.txt" || rec.URLs[1].Line != 2 {
		t.Errorf("URL: %+v", rec.URLs)
	}

	// Ошибка YAML несёт имя файла и строку.
	rec = Recognize(Input{Files: []File{{Name: "a.yaml", Content: "proxies:\n  - name: a\n    type: [unclosed\n"}}})
	var found *LineError
	for i := range rec.Errors {
		if rec.Errors[i].File == "a.yaml" && rec.Errors[i].Err.Code == CodeYAMLError {
			found = &rec.Errors[i]
		}
	}
	if found == nil || found.Line < 1 {
		t.Errorf("yaml_error: %+v", rec.Errors)
	}
	// Файл с нераспознанной строкой не скрывает остальное.
	rec = Recognize(Input{Files: []File{{Name: "d.txt", Content: "что-то\n" + vless}}})
	if len(rec.Nodes) != 1 || rtErrorAt(rec, "d.txt", 1, CodeUnrecognized) == nil {
		t.Errorf("файл со смешанным содержимым: %+v", rec)
	}
	rtCheckFragments(t, rec)
}

func TestRouterRecognizeDedupe(t *testing.T) {
	routertest.Current(t)

	vless := rtLink(t, "vless_ws")
	renamed := vless[:strings.Index(vless, "#")] + "#Other%20name"
	rec := Recognize(Input{Text: vless + "\n" + vless + "\n" + vless + "\n" + renamed + "\n"})
	if len(rec.Nodes) != 1 || rec.Collapsed != 3 || rec.Nodes[0].Text != vless {
		t.Errorf("узлов %d, схлопнуто %d (ожидалось 1 и 3)", len(rec.Nodes), rec.Collapsed)
	}
	// Порядок ввода сохраняется для разных узлов.
	trojan := rtLink(t, "trojan_grpc")
	rec = Recognize(Input{Text: trojan + "\n" + vless + "\n" + trojan + "\n"})
	if len(rec.Nodes) != 2 || rec.Collapsed != 1 || rec.Nodes[0].Text != trojan || rec.Nodes[1].Text != vless {
		t.Errorf("порядок или дубли: %d узлов, схлопнуто %d", len(rec.Nodes), rec.Collapsed)
	}
}

func TestRouterRecognizeLimits(t *testing.T) {
	routertest.Current(t)

	// 11 URL: десять в списке, на одиннадцатом — too_many_urls.
	var urls []string
	for i := 1; i <= 11; i++ {
		urls = append(urls, "https://sub.example/s/"+strconv.Itoa(i))
	}
	vless := rtLink(t, "vless_ws")
	rec := Recognize(Input{Text: strings.Join(urls, "\n") + "\n" + vless})
	if len(rec.URLs) != MaxURLs || len(rec.Nodes) != 1 {
		t.Errorf("URL %d, узлов %d: остальное должно обрабатываться", len(rec.URLs), len(rec.Nodes))
	}
	if e := rtErrorAt(rec, "", 11, CodeTooManyURLs); e == nil || e.Fragment != "https://sub.example" {
		t.Errorf("too_many_urls: %+v", rec.Errors)
	}

	// Больше 1 МиБ — одна ошибка и пустой результат.
	big := strings.Repeat("x", MaxInputBytes+1)
	rec = Recognize(Input{Text: big})
	if len(rec.Errors) != 1 || rec.Errors[0].Err.Code != CodeTooLarge || len(rec.Nodes)+len(rec.URLs) != 0 {
		t.Errorf("больше 1 МиБ: %+v", rec.Errors)
	}
	// Предел суммарный: вставка плюс файл.
	rec = Recognize(Input{Text: big[:MaxInputBytes/2+1], Files: []File{{Name: "f.txt", Content: big[:MaxInputBytes/2+1]}}})
	if len(rec.Errors) != 1 || rec.Errors[0].Err.Code != CodeTooLarge {
		t.Errorf("суммарно больше 1 МиБ: %+v", rec.Errors)
	}
	// 5001 строка.
	rec = Recognize(Input{Text: strings.Repeat("a\n", MaxInputLines)})
	if len(rec.Errors) != 1 || rec.Errors[0].Err.Code != CodeTooLarge || rec.Errors[0].Err.Params["unit"] != "lines" {
		t.Errorf("5001 строка: %+v", rec.Errors)
	}
	// Ровно 5000 строк — в пределах.
	rec = Recognize(Input{Text: strings.Repeat("a\n", MaxInputLines-1) + "a"})
	if len(rec.Errors) != MaxInputLines {
		t.Errorf("5000 строк: ошибок %d, ожидалось %d нераспознанных", len(rec.Errors), MaxInputLines)
	}
}

func TestRouterRecognizeFragments(t *testing.T) {
	routertest.Current(t)

	// Длинная строка: фрагмент не длиннее 80 символов и заканчивается «…».
	long := strings.Repeat("слово ", 40)
	rec := Recognize(Input{Text: long})
	if len(rec.Errors) != 1 || utf8.RuneCountInString(rec.Errors[0].Fragment) != MaxFragment || !strings.HasSuffix(rec.Errors[0].Fragment, "…") {
		t.Errorf("длинный фрагмент: %+v", rec.Errors)
	}
	// Ссылка с секретами в userinfo и запросе: во фрагменте нет ни того, ни другого.
	text := "ftp://user:topsecretpass@h.example:2121/path?token=abcdef123456\n" +
		"vmess://eyJhZGQiOiJub2RlMS5leGFtcGxlIiwicG9ydCI6IjQ0MyJ9\n" +
		"password=hunter2hunter2 token=abcdef123456\n"
	rec = Recognize(Input{Text: text})
	if len(rec.Errors) != 3 {
		t.Fatalf("ошибок %d: %+v", len(rec.Errors), rec.Errors)
	}
	if e := rtErrorAt(rec, "", 1, CodeUnknownScheme); e == nil || e.Fragment != "ftp://h.example:2121" {
		t.Errorf("ftp: %+v", e)
	}
	rtCheckFragments(t, rec, "topsecretpass", "abcdef123456", "hunter2hunter2", "eyJhZGQi")
}

// Исходный текст узла (D-13): повторное распознавание Text даёт один узел с той
// же идентичностью, а Format — один из link, json, yaml, wg.
func TestRouterRecognizeTextAndFormat(t *testing.T) {
	routertest.Current(t)

	var all []Node
	for _, l := range rtAllLinks(t) {
		if n, err := ParseLink(l); err == nil {
			all = append(all, n)
		}
	}
	for _, f := range []string{"xray_outbounds.json", "real_xray.json", "singbox.json", "clash_anchors.yaml", "real_clash.yaml", "wg_multi.conf", "wg_awg31.conf"} {
		res, err := ParseBody(rtInput(t, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		all = append(all, res.Nodes...)
	}
	if len(all) < 20 {
		t.Fatalf("мало узлов для проверки: %d", len(all))
	}
	formats := map[string]bool{}
	for i, n := range all {
		formats[n.Format] = true
		if n.Text == "" || n.Format == "" {
			t.Errorf("узел %d (%s/%s): пустые Text или Format", i, n.Protocol, n.Origin)
			continue
		}
		// Ссылка http-прокси неотличима от URL подписки (D-11: каждый http(s) URL
		// — элемент подписки), поэтому в общем вводе она URL; правка узла по
		// одному узлу идёт отдельным путём (edit_single_node).
		if n.Origin == OriginLink && n.Protocol == "http" {
			rec := Recognize(Input{Text: n.Text})
			if len(rec.URLs) != 1 || len(rec.Nodes) != 0 {
				t.Errorf("ссылка http-прокси: URL %d, узлов %d", len(rec.URLs), len(rec.Nodes))
			}
			continue
		}
		rec := Recognize(Input{Text: n.Text})
		if len(rec.Nodes) != 1 || len(rec.Errors) != 0 || len(rec.URLs) != 0 {
			t.Errorf("узел %d (%s, %s): повторное распознавание Text: узлов %d, ошибки %+v", i, n.Protocol, n.Format, len(rec.Nodes), rec.Errors)
			continue
		}
		if rec.Nodes[0].Identity() != n.Identity() {
			t.Errorf("узел %d (%s, %s): идентичность изменилась", i, n.Protocol, n.Format)
		}
	}
	for _, want := range []string{"link", "json", "yaml", "wg"} {
		if !formats[want] {
			t.Errorf("формат %s не встретился в данных", want)
		}
	}
}
