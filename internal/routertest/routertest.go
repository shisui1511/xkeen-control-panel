//go:build router

// Package routertest — общие помощники Go-тестов, которые запускаются на самом
// устройстве (тег сборки router, запуск через make router-test). Пакет не входит
// в бинарник продукта: без тега он пуст.
//
// Источник истины в таких тестах — вердикт системы (iptables, ndmc, XKeen, ядро),
// а не эталонный файл или таблица «вход → выход».
package routertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Target — цель, на которой запущен тест. Значения передаёт scripts/router/go-suite.sh.
type Target struct {
	// Arch — архитектура цели (arm64, mipsle).
	Arch string
	// Core — активное ядро цели (xray, mihomo).
	Core string
	// WorkDir — рабочий каталог теста на устройстве; всё временное — только в нём.
	WorkDir string
}

// Current читает цель из окружения. Без переменных тест запущен мимо make router-test.
func Current(t testing.TB) Target {
	t.Helper()
	tg := Target{
		Arch:    os.Getenv("XCP_RT_ARCH"),
		Core:    os.Getenv("XCP_RT_CORE"),
		WorkDir: os.Getenv("XCP_RT_WORKDIR"),
	}
	if tg.Arch == "" || tg.WorkDir == "" {
		t.Fatal("тест запускается только через make router-test")
	}
	return tg
}

// KnownMark — метка известного падения: селектор целей и slug todo.
// Пустая метка не применяется ни к одной цели.
type KnownMark struct {
	Selector string
	Slug     string
}

// Known создаёт метку известного падения. Селектор: «*», «<arch>», «<arch>/<ядро>»
// или «*/<ядро>»; slug — имя файла todo в .planning/todos/pending без расширения.
// Аргументы всегда строковые литералы: их ищет scripts/router/check-known.sh.
func Known(selector, slug string) KnownMark {
	return KnownMark{Selector: selector, Slug: slug}
}

// Applies сообщает, относится ли метка к цели. Грамматика селектора совпадает
// с rt_selector_match в scripts/router/lib.sh.
func (k KnownMark) Applies(tg Target) bool {
	if k.Selector == "" || k.Slug == "" {
		return false
	}
	sel := k.Selector
	switch {
	case sel == "*":
		return true
	case strings.HasPrefix(sel, "*/"):
		return strings.TrimPrefix(sel, "*/") == tg.Core
	case strings.Contains(sel, "/"):
		arch, core, _ := strings.Cut(sel, "/")
		return arch == tg.Arch && core == tg.Core
	default:
		return sel == tg.Arch
	}
}

// Маркеры вывода, по которым go-suite.sh отличает KNOWN и XPASS от PASS и FAIL.
const (
	markerKnown = "KNOWN-FAILURE slug="
	markerXPass = "XPASS slug="
)

// Verdict фиксирует итог утверждения с учётом метки известного падения.
//   - метка не применяется: ok=false — провал теста;
//   - метка применяется, ok=false — известное падение (KNOWN), тест не краснеет;
//   - метка применяется, ok=true — XPASS: метка больше не нужна, тест краснеет.
func Verdict(t *testing.T, ok bool, mark KnownMark, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if !mark.Applies(Current(t)) {
		if !ok {
			t.Errorf("%s", msg)
		}
		return
	}
	if ok {
		t.Errorf("%s%s: метка больше не нужна — снимите её и закройте todo", markerXPass, mark.Slug)
		return
	}
	t.Logf("%s%s: %s", markerKnown, mark.Slug, msg)
}

// Exec запускает системную команду и возвращает её stdout, stderr и код выхода.
// Код -1 — команда не запустилась (нет бинарника) или убита по контексту.
func Exec(ctx context.Context, name string, args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		code = -1
		if errOut.Len() == 0 {
			errOut.WriteString(err.Error())
		}
	}
	return out.String(), errOut.String(), code
}
