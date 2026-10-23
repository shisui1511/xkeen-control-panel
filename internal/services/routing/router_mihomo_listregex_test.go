//go:build router

package routing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Регулярные выражения с запятой из списка dat доходят до Mihomo: строку
// DOMAIN-REGEX текстового провайдера classical Mihomo собирает по всем полям после
// типа. Запись, которую строкой провайдера не записать (со «/»), отбрасывается и
// считается: у правила предупреждение list_dropped. Список собирается в каталоге
// теста (в dat стенда выражений с запятой может не быть), вердикт — ruleCount
// провайдера в отдельном процессе Mihomo. Ядро стенда не читается и не
// переключается, метка ядра не нужна.

const (
	rtlrFile = "geosite_xcprt.dat"
	rtlrTag  = "XCPRT"
)

// rtlrWriteDat пишет GeoSiteList с одной категорией: записи — пары (тип Domain,
// значение).
func rtlrWriteDat(t *testing.T, path string, domains [][2]any) {
	t.Helper()
	var site []byte
	site = protowire.AppendTag(site, 1, protowire.BytesType)
	site = protowire.AppendString(site, rtlrTag)
	for _, d := range domains {
		var dom []byte
		dom = protowire.AppendTag(dom, 1, protowire.VarintType)
		dom = protowire.AppendVarint(dom, uint64(d[0].(int)))
		dom = protowire.AppendTag(dom, 2, protowire.BytesType)
		dom = protowire.AppendString(dom, d[1].(string))
		site = protowire.AppendTag(site, 2, protowire.BytesType)
		site = protowire.AppendBytes(site, dom)
	}
	var list []byte
	list = protowire.AppendTag(list, 1, protowire.BytesType)
	list = protowire.AppendBytes(list, site)
	if err := os.WriteFile(path, list, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRouterMihomoListRegexComma(t *testing.T) {
	dir := rtWorkSubdir(t, "rl-")
	// Типы Domain в dat: 1 — regexp, 2 — домен с поддоменами.
	rtlrWriteDat(t, filepath.Join(dir, rtlrFile), [][2]any{
		{1, `^(ab){1,2}\.rt-comma\.test$`},
		{1, `^a(,b)?\.rt-comma\.test$`},
		{1, `^x/y\.rt-comma\.test$`},
		{2, "rt-root.test"},
	})
	ix := lists.NewIndex(dir)
	cat, err := ix.Extract(rtlrFile, rtlrTag)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	commas := 0
	for _, l := range cat.Classical {
		if strings.HasPrefix(l, "DOMAIN-REGEX,") && strings.Contains(strings.TrimPrefix(l, "DOMAIN-REGEX,"), ",") {
			commas++
		}
	}
	if len(cat.Classical) != 2 || commas != 2 || len(cat.Domains) != 1 || cat.Dropped != 1 {
		t.Fatalf("выгрузка: classical %d (с запятой %d), доменов %d, отброшено %d; ожидалось 2 (2), 1, 1",
			len(cat.Classical), commas, len(cat.Domains), cat.Dropped)
	}

	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtWithDeps(func(d *Deps) { d.Lists = ix }))
	e.writeUserProfile(t)
	e.addMyLinks(t, "vless_ws")
	_, rule, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{{File: rtlrFile, Tag: rtlrTag, Kind: lists.KindSite}}}},
	})
	if err != nil {
		t.Fatalf("SaveRule: %v", err)
	}

	view, err := e.svc.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	rv := rtcRuleView(t, view, rule.ID)
	dropped := ""
	for _, w := range rv.Warnings {
		if w.Code == warnListDropped {
			dropped = w.Params["dropped"]
		}
	}
	if dropped != "1" {
		t.Errorf("предупреждение list_dropped: %q, ожидалось 1 (предупреждения %+v)", dropped, rv.Warnings)
	}
	if len(rv.Issues) != 0 {
		t.Errorf("отброшенные записи стали проблемой: %+v", rv.Issues)
	}

	e.takeAndApply(t)
	rtmtStart(t, e, core, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	n, err := core.svc.ProviderCount(ctx, "rules", ProviderName(rtlrFile, rtlrTag, listPartClassical))
	if err != nil {
		t.Fatalf("ruleCount провайдера: %v", err)
	}
	if n != len(cat.Classical) {
		t.Errorf("Mihomo принял %d из %d строк DOMAIN-REGEX с запятой", n, len(cat.Classical))
	}
}
