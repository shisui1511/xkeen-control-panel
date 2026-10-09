#!/bin/sh
# scripts/router/check-known.sh [--slug <slug>] — метки «известное падение» ссылаются на живые todo.
#
# Каждая метка (известное падение, D-12) называет slug todo. Файл
# <основная копия>/.planning/todos/pending/<slug>.md должен существовать: закрытый
# (перенесённый из pending) или несуществующий todo — падение (D-13). Метка, оставшаяся
# после закрытия todo, больше не может «тихо» пережить исправление.
#
# Источники меток (поиск по рабочему дереву, а не git grep: в worktree новые файлы ещё
# не в индексе):
#   scripts/router/known-failures                 третье поле записи
#   internal/**/*_test.go                         routertest.Known("<селектор>", "<slug>")
#   frontend/tests/router/**/*.ts                 knownFailure('<селектор>', '<slug>')
# Вызовы могут быть перенесены на другую строку (prettier, gofmt), поэтому файл перед
# поиском склеивается в одну строку; строки-комментарии в поиске не участвуют.
#
# --slug <slug>   проверить один slug (для отладки и проверки направления падения).
#
# Если каталога .planning в основной копии нет (CI, чужая машина), проверка пропускается
# с предупреждением и кодом 0: в CI она не выполняется (D-13).
#
# Код выхода: 0 — все метки живые (или проверка пропущена), 1 — есть мёртвые метки или
# ошибки в known-failures, 2 — неверные аргументы.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

ONLY_SLUG=""
case "${1:-}" in
  '') ;;
  --slug)
    if [ $# -ne 2 ] || [ -z "$2" ]; then
      echo "usage: check-known.sh [--slug <slug>]" >&2
      exit 2
    fi
    ONLY_SLUG=$2
    ;;
  *)
    echo "usage: check-known.sh [--slug <slug>]" >&2
    exit 2
    ;;
esac

ROOT=$(rt_repo_root)
MAIN=$(rt_main_root)
PENDING="$MAIN/.planning/todos/pending"
TAB=$(printf '\t')

if [ ! -d "$MAIN/.planning" ]; then
  echo "check-known: нет каталога .planning в основной копии — метки не проверяются (CI или чужая машина)" >&2
  exit 0
fi

BAD=0
SEEN=$(mktemp)
trap 'rm -f "$SEEN"' EXIT

# Проверка одного slug: check_slug <slug> <источник>
check_slug() {
  _s=$1
  _src=$2
  case "$_s" in
    '' | [!a-z0-9]* | *[!a-z0-9-]*)
      echo "check-known: недопустимый slug '$_s' ($_src)" >&2
      BAD=1
      return 0
      ;;
  esac
  if grep -qxF "$_s" "$SEEN"; then
    return 0
  fi
  printf '%s\n' "$_s" >>"$SEEN"
  if [ ! -f "$PENDING/$_s.md" ]; then
    echo "метка ссылается на закрытый или несуществующий todo: $_s ($_src)" >&2
    BAD=1
  fi
}

if [ -n "$ONLY_SLUG" ]; then
  check_slug "$ONLY_SLUG" "аргумент --slug"
  [ "$BAD" = 0 ] || exit 1
  echo "check-known: todo $ONLY_SLUG есть в pending"
  exit 0
fi

COUNT=0

# 1. scripts/router/known-failures: <проверка> TAB <селектор> TAB <slug>
KF="$ROOT/scripts/router/known-failures"
if [ -f "$KF" ]; then
  _n=0
  while IFS= read -r _line || [ -n "$_line" ]; do
    _n=$((_n + 1))
    case "$_line" in '' | '#'*) continue ;; esac
    _f1=$(printf '%s' "$_line" | cut -d"$TAB" -f1)
    _f2=$(printf '%s' "$_line" | cut -d"$TAB" -f2)
    _f3=$(printf '%s' "$_line" | cut -d"$TAB" -f3)
    _f4=$(printf '%s' "$_line" | cut -d"$TAB" -f4)
    if [ -z "$_f1" ] || [ -z "$_f2" ] || [ -z "$_f3" ] || [ -n "$_f4" ] || [ "$(printf '%s' "$_line" | tr -cd "$TAB" | wc -c | tr -d ' ')" != 2 ]; then
      echo "check-known: scripts/router/known-failures:$_n: нужны ровно три поля через табуляцию (проверка, селектор, slug)" >&2
      BAD=1
      continue
    fi
    case "$_f1" in
      smoke:?* | reboot:?*) ;;
      *)
        echo "check-known: scripts/router/known-failures:$_n: проверка должна начинаться с smoke: или reboot: ($_f1)" >&2
        BAD=1
        ;;
    esac
    COUNT=$((COUNT + 1))
    check_slug "$_f3" "scripts/router/known-failures:$_n"
  done <"$KF"
fi

# 2. и 3. вызовы в Go и Playwright. Файл склеивается в одну строку (без строк-комментариев),
# затем выбираются вызовы с двумя строковыми аргументами и берётся второй — slug.
scan_calls() {
  # scan_calls <каталог> <маска файлов> — выводит «файл<TAB>slug»
  [ -d "$1" ] || return 0
  grep -rlE 'routertest\.Known|knownFailure' --include="$2" "$1" 2>/dev/null | while IFS= read -r _file; do
    grep -vE '^[[:space:]]*(//|/\*|\*)' "$_file" | tr '\n' ' ' |
      grep -oE "(routertest\.Known|knownFailure)\([[:space:]]*[\"'][^\"']*[\"'][[:space:]]*,[[:space:]]*[\"'][a-z0-9][a-z0-9-]*[\"']" |
      sed -E "s/.*,[[:space:]]*[\"']([a-z0-9-]+)[\"']\$/\1/" |
      while IFS= read -r _slug; do
        printf '%s\t%s\n' "${_file#"$ROOT"/}" "$_slug"
      done
  done
}

CALLS=$(mktemp)
trap 'rm -f "$SEEN" "$CALLS"' EXIT
{
  scan_calls "$ROOT/internal" '*_test.go'
  scan_calls "$ROOT/frontend/tests/router" '*.ts'
} >"$CALLS"
while IFS="$TAB" read -r _file _slug; do
  COUNT=$((COUNT + 1))
  check_slug "$_slug" "$_file"
done <"$CALLS"

if [ "$BAD" != 0 ]; then
  exit 1
fi
echo "check-known: меток проверено: $COUNT, все ссылаются на todo из pending"
