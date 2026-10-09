//go:build router

package nodes

import (
	"os"
	"path/filepath"
	"testing"
)

// Fuzz-цели разборщиков (T-145-04): произвольный ввод не роняет разбор. Запуск
// на ПК, вердикт — отсутствие паники:
//
//	go test -tags router ./internal/nodes -run '^$' -fuzz '^FuzzParseBody$' -fuzztime 30s
//
// Узел, который разобрался, обязан пройти оба эмиттера без паники (отказ с кодом
// допустим). Сиды — входные файлы testdata/inputs и ссылки testdata/links.

func fuzzSeeds(f *testing.F, dir string) {
	f.Helper()
	files, _ := filepath.Glob(filepath.Join("testdata", dir, "*"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil && len(data) < 1<<16 {
			f.Add(data)
		}
	}
}

// fuzzEmit прогоняет узлы через эмиттеры и построение тегов и ключей.
func fuzzEmit(res ParseResult) {
	for i, n := range res.Nodes {
		key := n.Key("ab12cd34")
		_, _ = XrayOutbound(n, XrayTag("ab12cd34", key))
		_, _ = ClashProxy(n, "n"+string(rune('a'+i%26)))
		_, _ = EmitXray(n)
		_, _ = EmitClash(n)
	}
	_, _ = Uniq("ab12cd34", res.Nodes)
}

func FuzzParseLink(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "links", "*.txt"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(string(data))
		}
	}
	f.Fuzz(func(t *testing.T, s string) {
		if n, err := ParseLink(s); err == nil {
			fuzzEmit(ParseResult{Nodes: []Node{n}})
		}
	})
}

func FuzzParseClash(f *testing.F) {
	fuzzSeeds(f, "inputs")
	f.Fuzz(func(t *testing.T, data []byte) {
		if res, err := ParseClash(data); err == nil {
			fuzzEmit(res)
		}
	})
}

func FuzzParseXray(f *testing.F) {
	fuzzSeeds(f, "inputs")
	f.Fuzz(func(t *testing.T, data []byte) {
		if res, err := ParseXray(data); err == nil {
			fuzzEmit(res)
		}
	})
}

func FuzzParseBody(f *testing.F) {
	fuzzSeeds(f, "inputs")
	fuzzSeeds(f, "links")
	f.Fuzz(func(t *testing.T, data []byte) {
		if res, err := ParseBody(data); err == nil {
			fuzzEmit(res)
		}
	})
}
