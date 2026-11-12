//go:build router

package routertest

import (
	"strings"
	"testing"
)

// OneCore — Current плюс метка «одно ядро» (D-01…D-04): тест зависит от ядра только
// перезапуском или файлом, которые на обоих ядрах проверяет названный тест covers того
// же семейства (тот же пакет и та же router-area) с меткой CoreSpecific. Вызов заменяет
// CoreSpecific и выполняет правило «тело, читающее ядро, размечено», но метку ядра не
// ставит: в changed тест идёт в одну ветвь с ядром из SHA HEAD (D-03), а в полном
// прогоне вторая ветвь его не запускает.
//
// Метку читают статически (scripts/router/core-marks.cjs, страж scripts/check-core-marks.js),
// поэтому вызывать её нужно прямо в теле TestRouter*, причину и covers — непустыми
// строковыми литералами; covers — имя TestRouter* с CoreSpecific, не в карантине и не
// с меткой «одно ядро». Пустая причина или covers и ядро вне xray/mihomo — провал.
func OneCore(t testing.TB, reason, covers string) Target {
	t.Helper()
	if strings.TrimSpace(reason) == "" {
		t.Fatalf("метка «одно ядро» без причины: нужна зависимость теста от ядра")
	}
	if strings.TrimSpace(covers) == "" {
		t.Fatalf("метка «одно ядро» без покрытия: нужен тест того же семейства с CoreSpecific")
	}
	tg := Current(t)
	if tg.Core != "xray" && tg.Core != "mihomo" {
		t.Fatalf("тест с меткой «одно ядро», а ядро ветви не определено: %q", tg.Core)
	}
	return tg
}
