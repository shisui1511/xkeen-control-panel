//go:build router

package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"testing"
	"time"
)

// Состав групп Mihomo совпадает с Resolve: фильтры группы считаются по именам для
// показа движком Go, а в профиль пишется список точных имён файла провайдера, а
// не фильтры как есть (иначе Mihomo применил бы их к именам после uniqueName и
// своим движком). Отдельный процесс Mihomo в каталоге теста (как в
// router_mihomo_traffic_test.go): ядро стенда не читается и не переключается,
// метка ядра не нужна. Вердикт — поле all группы в Clash API процесса.

// rtmGroupAll возвращает участников группы живого процесса: GET /proxies/{группа},
// поле all.
func rtmGroupAll(t *testing.T, c *rtMihomoCore, group string) []string {
	t.Helper()
	info, err := c.svc.ParseControllerConfig()
	if err != nil {
		t.Fatalf("контроллер процесса: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/proxies/"+url.PathEscape(group), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}
	resp, err := c.svc.GetHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("GET /proxies/%s: %v", group, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /proxies/%s: код %d", group, resp.StatusCode)
	}
	var doc struct {
		All []string `json:"all"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("GET /proxies/%s: разбор ответа: %v", group, err)
	}
	sort.Strings(doc.All)
	return doc.All
}

// Два узла с одним именем: в файле провайдера Mihomo второй получает суффикс
// « 2», для показа и Resolve оба остаются «rt-dup». Выражение «^rt-dup$» и
// подстрока «dup 2» отбирают по Resolve одно, а по именам провайдера — другое;
// группа в Mihomo обязана содержать ровно узлы Resolve.
func TestRouterMihomoGroupComposition(t *testing.T) {
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithMihomoCore(core), rtWithKernels(rtMihomoOnly))
	e.writeUserProfile(t)
	e.addMyRaw(t,
		"socks5://127.0.0.1:9#rt-dup",
		"socks5://127.0.0.1:10#rt-dup",
		"socks5://127.0.0.1:11#rt-other",
	)
	cat := e.catalog(t).Mihomo
	dups := 0
	for _, n := range cat {
		if n.Display == "rt-dup" && n.MihomoName != n.Display {
			dups++
		}
	}
	if len(cat) != 3 || dups != 1 {
		t.Fatalf("в каталоге Mihomo %d узлов, с суффиксом имени %d; ожидалось 3 и 1", len(cat), dups)
	}

	_, exact, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Точное имя", Mode: ModeAuto, Sources: GroupSources{All: true},
		Regex: true, Include: []string{"^rt-dup$"},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Точное имя): %v", err)
	}
	_, manual, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Без дубля", Mode: ModeManual, Sources: GroupSources{All: true},
		Exclude: []string{"dup 2"},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Без дубля): %v", err)
	}
	_, other, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Другой", Mode: ModeAuto, Sources: GroupSources{All: true},
		Include: []string{"other"},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Другой): %v", err)
	}
	for i, g := range []Group{exact, manual, other} {
		if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
			Enabled: true, Target: Target{Kind: KindGroup, ID: g.ID},
			Conds: []Cond{{Type: CondDomain, Values: []string{"compose" + string(rune('a'+i)) + ".example.test"}}},
		}); err != nil {
			t.Fatalf("SaveRule (%s): %v", g.Name, err)
		}
	}
	e.takeAndApply(t)
	rtmtStart(t, e, core, nil)

	for _, g := range []Group{exact, manual, other} {
		var want []string
		for _, n := range Resolve(g, cat, nil).Nodes {
			want = append(want, n.MihomoName)
		}
		sort.Strings(want)
		got := rtmGroupAll(t, core, g.Name)
		if !slices.Equal(got, want) {
			t.Errorf("группа «%s»: в Mihomo участников %d, по Resolve %d (Mihomo %q, Resolve %q)", g.Name, len(got), len(want), got, want)
		}
	}
}
