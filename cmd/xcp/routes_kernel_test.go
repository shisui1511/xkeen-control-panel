package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
)

// protectedRouteRe ловит литералы регистрации защищённых маршрутов в main.go;
// группа 2 непуста, когда за литералом идёт конкатенация (префикс + действие).
var protectedRouteRe = regexp.MustCompile(`HandleProtected\("(/api/[^"]+)"(\+)?`)

const policyHint = "добавьте запись в kernelRoutePolicies (internal/handlers/kernel_gate.go) с режимом и причиной"

type protectedRoute struct {
	pattern string
	concat  bool
	offset  int
}

func readProtectedRoutes(t *testing.T) (string, []protectedRoute) {
	t.Helper()
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("не удалось прочитать main.go: %v", err)
	}
	src := string(raw)
	var routes []protectedRoute
	for _, m := range protectedRouteRe.FindAllStringSubmatchIndex(src, -1) {
		routes = append(routes, protectedRoute{
			pattern: src[m[2]:m[3]],
			concat:  m[4] >= 0,
			offset:  m[0],
		})
	}
	if len(routes) == 0 {
		t.Fatal("в main.go не найдено ни одного HandleProtected с литералом /api/…")
	}
	return src, routes
}

func isGated(pattern string) bool {
	for _, prefix := range handlers.KernelGatedPrefixes() {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
	}
	return false
}

func TestRouteKernelTable_EveryGatedRouteHasPolicy(t *testing.T) {
	_, routes := readProtectedRoutes(t)
	gated := 0
	for _, r := range routes {
		if !isGated(r.pattern) {
			continue
		}
		gated++
		if !r.concat {
			if _, ok := handlers.KernelRoutePolicyFor(r.pattern); !ok {
				t.Errorf("маршрут %s зарегистрирован под префиксом ядра без политики: %s", r.pattern, policyHint)
			}
			continue
		}
		// Конкатенация: действия собираются в цикле, у каждого своя запись
		// с этим префиксом (проверяем известные действия профилей).
		found := false
		for _, action := range []string{"create", "rename", "delete", "activate", "adopt"} {
			if _, ok := handlers.KernelRoutePolicyFor(r.pattern + action); ok {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("для префикса %s+действие нет ни одной записи в таблице: %s", r.pattern, policyHint)
		}
	}
	if gated == 0 {
		t.Fatal("в main.go не найдено маршрутов под префиксами ядер — сканер сломан")
	}
}

func TestRouteKernelTable_NoStalePolicies(t *testing.T) {
	_, routes := readProtectedRoutes(t)
	registered := map[string]bool{}
	var concatPrefixes []string
	for _, r := range routes {
		if r.concat {
			concatPrefixes = append(concatPrefixes, r.pattern)
		} else {
			registered[r.pattern] = true
		}
	}
	// Обратная сторона предыдущего теста: запись таблицы без регистрации в
	// main.go (ни литерал, ни префикс конкатенации) — устаревшая.
	known := handlers.KernelRoutePolicyKeys()
	if len(known) == 0 {
		t.Fatal("таблица политик пуста")
	}
	for _, key := range known {
		if registered[key] {
			continue
		}
		matched := false
		for _, prefix := range concatPrefixes {
			if strings.HasPrefix(key, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("запись таблицы %s не соответствует ни одной регистрации в main.go: удалите устаревшую запись из kernelRoutePolicies (internal/handlers/kernel_gate.go)", key)
		}
	}
}

func TestRouteKernelTable_WrapperSetBeforeRoutes(t *testing.T) {
	src, routes := readProtectedRoutes(t)
	wrapAt := strings.Index(src, "SetProtectedWrapper(api.KernelRouteWrapper)")
	if wrapAt < 0 {
		t.Fatal("в main.go нет srv.SetProtectedWrapper(api.KernelRouteWrapper)")
	}
	for _, r := range routes {
		if isGated(r.pattern) && r.offset < wrapAt {
			t.Fatalf("маршрут %s зарегистрирован раньше SetProtectedWrapper: гейт его не увидит", r.pattern)
		}
	}
}
