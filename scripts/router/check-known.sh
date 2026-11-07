#!/bin/sh
# scripts/router/check-known.sh [--slug <slug> | --set-until <ГГГГ-ММ-ДД> [--all]] —
# метки «известное падение» и карантина ссылаются на живые todo с непросроченным сроком.
#
# Каждая метка (известное падение, D-12; карантин, D-17) называет slug todo. Файл
# <основная копия>/.planning/todos/pending/<slug>.md должен существовать: закрытый
# (перенесённый из pending) или несуществующий todo — падение (D-13). Метка, оставшаяся
# после закрытия todo, больше не может «тихо» пережить исправление.
#
# Срок метки (D-18): у todo во frontmatter (между первой парой строк «---») обязано быть
# поле «until: ГГГГ-ММ-ДД». Нет поля, дата не по шаблону или срок прошёл (дата раньше
# сегодняшней) — падение. Метки в коде при этом не меняются: продление — правка until в
# todo с причиной, иначе метку пора снять (исправить дефект или нестабильность).
#
# Источники меток (поиск по рабочему дереву, а не git grep: в worktree новые файлы ещё
# не в индексе):
#   scripts/router/known-failures                 третье поле записи
#   internal/**/*_test.go                         routertest.Known("<селектор>", "<slug>")
#                                                 routertest.Quarantine(t, "<slug>") — карантин (D-17)
#   frontend/tests/router/**/*.ts                 knownFailure('<селектор>', '<slug>')
#                                                 quarantine('<slug>') — карантин (D-17)
# Вызовы могут быть перенесены на другую строку (prettier, gofmt), поэтому файл перед
# поиском склеивается в одну строку; строки-комментарии в поиске не участвуют.
#
# --slug <slug>                 проверить один slug (для отладки и проверки направления
#                               падения): todo в pending и его срок.
# --set-until <дата> [--all]    вписать «until: <дата>» во frontmatter каждого todo, на
#                               который ссылается метка и у которого срока ещё нет (строка
#                               перед закрывающей «---»); с --all — и заменить имеющийся
#                               срок. Печатает число изменённых todo. При слиянии фазы:
#                               --set-until <дата слияния + 14 дней> --all.
#
# Проверка локальная: run.sh вызывает скрипт до блокировки, ship:pre — перед PR. Если
# каталога .planning в основной копии нет (CI, чужая машина), проверка пропускается
# с предупреждением и кодом 0: в CI она не выполняется (D-13, D-18).
#
# Код выхода: 0 — все метки живые и в сроке (или проверка пропущена; --set-until выполнен),
# 1 — есть мёртвые или просроченные метки, todo без срока, ошибки в known-failures,
# 2 — неверные аргументы (в том числе дата --set-until не по шаблону).

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

USAGE="usage: check-known.sh [--slug <slug> | --set-until <ГГГГ-ММ-ДД> [--all]]"

# valid_date <дата> — шаблон ГГГГ-ММ-ДД с допустимыми месяцем и днём
valid_date() {
  case "$1" in
    [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
    *) return 1 ;;
  esac
  _m=$(printf '%s' "$1" | cut -d- -f2)
  _d=$(printf '%s' "$1" | cut -d- -f3)
  [ "$_m" -ge 1 ] 2>/dev/null && [ "$_m" -le 12 ] && [ "$_d" -ge 1 ] && [ "$_d" -le 31 ]
}

MODE=check
ONLY_SLUG=""
SET_DATE=""
SET_ALL=0
case "${1:-}" in
  '') ;;
  --slug)
    if [ $# -ne 2 ] || [ -z "$2" ]; then
      echo "$USAGE" >&2
      exit 2
    fi
    MODE=slug
    ONLY_SLUG=$2
    ;;
  --set-until)
    if [ $# -lt 2 ] || [ $# -gt 3 ] || { [ $# -eq 3 ] && [ "$3" != --all ]; }; then
      echo "$USAGE" >&2
      exit 2
    fi
    if ! valid_date "$2"; then
      echo "check-known: дата --set-until не по шаблону ГГГГ-ММ-ДД: $2" >&2
      exit 2
    fi
    MODE=set
    SET_DATE=$2
    [ $# -eq 3 ] && SET_ALL=1
    ;;
  *)
    echo "$USAGE" >&2
    exit 2
    ;;
esac

ROOT=$(rt_repo_root)
MAIN=$(rt_main_root)
PENDING="$MAIN/.planning/todos/pending"
TAB=$(printf '\t')
TODAY=$(date +%F)

if [ ! -d "$MAIN/.planning" ]; then
  if [ "$MODE" = set ]; then
    echo "check-known: нет каталога .planning в основной копии — сроки проставлять некуда" >&2
    exit 1
  fi
  echo "check-known: нет каталога .planning в основной копии — метки не проверяются (CI или чужая машина)" >&2
  exit 0
fi

BAD=0
SEEN=$(mktemp)
CALLS=$(mktemp)
trap 'rm -f "$SEEN" "$CALLS"' EXIT

# frontmatter <файл> — строки между первой парой «---» (пусто, если файл начинается не с «---»)
frontmatter() {
  tr -d '\r' <"$1" | sed -n '1{/^---$/!q;}; 1d; /^---$/q; p'
}

# todo_until <файл> — значение поля until из frontmatter (без кавычек); пусто — поля нет
todo_until() {
  frontmatter "$1" | sed -n 's/^until:[[:space:]]*//p' | head -n 1 |
    sed -e 's/[[:space:]]*$//' -e "s/^[\"']//" -e "s/[\"']\$//"
}

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
    return 0
  fi
  _until=$(todo_until "$PENDING/$_s.md")
  if [ -z "$_until" ]; then
    echo "метка без срока: $_s ($_src) — until: ГГГГ-ММ-ДД в todo" >&2
    BAD=1
  elif ! valid_date "$_until"; then
    echo "срок метки не по шаблону ГГГГ-ММ-ДД: $_s ($_src) — until: $_until" >&2
    BAD=1
  elif [ "$(printf '%s' "$_until" | tr -d -)" -lt "$(printf '%s' "$TODAY" | tr -d -)" ]; then
    echo "срок метки истёк $_until: $_s ($_src) — продлить в todo с причиной или снять метку" >&2
    BAD=1
  fi
}

if [ "$MODE" = slug ]; then
  check_slug "$ONLY_SLUG" "аргумент --slug"
  [ "$BAD" = 0 ] || exit 1
  echo "check-known: todo $ONLY_SLUG есть в pending, срок не истёк"
  exit 0
fi

# Сбор меток в $CALLS: строки «источник<TAB>slug».

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
    printf '%s\t%s\n' "scripts/router/known-failures:$_n" "$_f3" >>"$CALLS"
  done <"$KF"
fi

# 2. и 3. вызовы в Go и Playwright. Файл склеивается в одну строку (без строк-комментариев),
# затем выбираются вызовы меток и из каждого берётся slug (у Known и knownFailure — второй
# строковый аргумент, у карантина — единственный строковый).
scan_calls() {
  # scan_calls <каталог> <маска файлов> — выводит «файл<TAB>slug»
  [ -d "$1" ] || return 0
  grep -rlE 'routertest\.Known|knownFailure|routertest\.Quarantine|quarantine\(' --include="$2" "$1" 2>/dev/null | while IFS= read -r _file; do
    # пробел в начале: вызов quarantine в самом начале файла тоже предварён «не буквой»
    _flat=" $(grep -vE '^[[:space:]]*(//|/\*|\*)' "$_file" | tr '\n' ' ')"
    {
      # метки известного падения: два строковых аргумента, slug — второй
      printf '%s\n' "$_flat" |
        grep -oE "(routertest\.Known|knownFailure)\([[:space:]]*[\"'][^\"']*[\"'][[:space:]]*,[[:space:]]*[\"'][a-z0-9][a-z0-9-]*[\"']" |
        sed -E "s/.*,[[:space:]]*[\"']([a-z0-9-]+)[\"']\$/\1/"
      # карантин Go: routertest.Quarantine(t, "<slug>") — первый аргумент идентификатор теста
      printf '%s\n' "$_flat" |
        grep -oE "routertest\.Quarantine\([[:space:]]*[A-Za-z_][A-Za-z0-9_]*[[:space:]]*,[[:space:]]*\"[a-z0-9][a-z0-9-]*\"" |
        sed -E 's/.*"([a-z0-9-]+)"$/\1/'
      # карантин Playwright: quarantine('<slug>') — один строковый литерал; перед именем не
      # буква, не цифра и не «_», чтобы не путать со словами в других именах
      printf '%s\n' "$_flat" |
        grep -oE "[^A-Za-z0-9_\$]quarantine\([[:space:]]*[\"'][a-z0-9][a-z0-9-]*[\"']" |
        sed -E "s/.*[\"']([a-z0-9-]+)[\"']\$/\1/"
    } | while IFS= read -r _slug; do
      printf '%s\t%s\n' "${_file#"$ROOT"/}" "$_slug"
    done
  done
}

{
  scan_calls "$ROOT/internal" '*_test.go'
  scan_calls "$ROOT/frontend/tests/router" '*.ts'
} >>"$CALLS"

# --set-until: проставить срок todo, на которые ссылаются метки
if [ "$MODE" = set ]; then
  CHANGED=0
  while IFS="$TAB" read -r _src _slug; do
    case "$_slug" in
      '' | [!a-z0-9]* | *[!a-z0-9-]*)
        echo "check-known: недопустимый slug '$_slug' ($_src)" >&2
        BAD=1
        continue
        ;;
    esac
    grep -qxF "$_slug" "$SEEN" && continue
    printf '%s\n' "$_slug" >>"$SEEN"
    _todo="$PENDING/$_slug.md"
    if [ ! -f "$_todo" ]; then
      echo "метка ссылается на закрытый или несуществующий todo: $_slug ($_src)" >&2
      BAD=1
      continue
    fi
    if [ "$(tr -d '\r' <"$_todo" | sed -n '1p')" != "---" ] || [ "$(tr -d '\r' <"$_todo" | sed -n '2,$p' | grep -c '^---$')" = 0 ]; then
      echo "check-known: у todo нет frontmatter между строками «---»: $_slug" >&2
      BAD=1
      continue
    fi
    _cur=$(todo_until "$_todo")
    if [ -n "$_cur" ] && [ "$SET_ALL" = 0 ]; then
      continue
    fi
    [ "$_cur" = "$SET_DATE" ] && continue
    _tmp=$(mktemp)
    # правка только внутри frontmatter: строки until заменяются одной новой (или новая
    # вписывается перед закрывающей «---»), остальное — без изменений
    awk -v d="$SET_DATE" '
      fm == 0 && NR == 1 && /^---[[:space:]]*$/ { fm = 1; print; next }
      fm == 1 && /^until:/ { if (!done) { print "until: " d; done = 1 }; next }
      fm == 1 && /^---[[:space:]]*$/ { if (!done) print "until: " d; done = 1; fm = 2; print; next }
      { print }
    ' "$_todo" >"$_tmp"
    cat "$_tmp" >"$_todo"
    rm -f "$_tmp"
    CHANGED=$((CHANGED + 1))
  done <"$CALLS"
  echo "check-known: срок until: $SET_DATE вписан в todo: $CHANGED"
  [ "$BAD" = 0 ] || exit 1
  exit 0
fi

COUNT=0
while IFS="$TAB" read -r _src _slug; do
  COUNT=$((COUNT + 1))
  check_slug "$_slug" "$_src"
done <"$CALLS"

if [ "$BAD" != 0 ]; then
  exit 1
fi
echo "check-known: меток проверено: $COUNT, все ссылаются на todo из pending, сроки не истекли"
