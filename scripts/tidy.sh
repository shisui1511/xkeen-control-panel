#!/bin/sh
# scripts/tidy.sh — уборка за агентами на ПК: сборки, отчёты стенда, кэш релизов.
#
#   scripts/tidy.sh                  показ: что будет удалено и сколько места, ничего не меняется
#   scripts/tidy.sh --apply          выполнить ровно показанный план (make tidy APPLY=1)
#   scripts/tidy.sh --rotate-builds  только ротация сборок build/xcp_v*_<арх> (вызывает Makefile)
#   scripts/tidy.sh -h | --help      эта справка
#
# Пороги — переменные XCP_TIDY_* (умолчания и смысл — в шапке scripts/tidy-lib.sh).
# Показ и исполнение строятся по одному плану: по категориям, с действием, путём,
# размером и пропущенным с причиной. Пути — от корня репозитория.
#
# Коды выхода: 0 — готово; 1 — часть операций не удалась; 2 — неверные аргументы.
set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/router/lib.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/tidy-lib.sh"

APPLY=0
ROTATE_BUILDS=0
for _a in "$@"; do
  case "$_a" in
    --apply) APPLY=1 ;;
    --rotate-builds) ROTATE_BUILDS=1 ;;
    -h | --help)
      awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
      exit 0
      ;;
    *)
      echo "tidy: неизвестный аргумент: $_a" >&2
      exit 2
      ;;
  esac
done

# Ротация сборок для Makefile: сборка не должна падать из-за уборки.
if [ "$ROTATE_BUILDS" = 1 ]; then
  if _root=$(rt_repo_root 2>/dev/null) && [ -n "$_root" ]; then
    tidy_rotate_builds "$_root" || echo "tidy: предупреждение: ротация сборок не завершилась" >&2
  fi
  exit 0
fi

ROOT=$(rt_repo_root)
cd "$ROOT"

PLAN=$(mktemp)
trap 'rm -f "$PLAN"' EXIT

# категории показа через "|" (в названиях есть пробелы), порядок = порядок показа
CATEGORIES="сборки"

# --- построение плана: одна функция на категорию, записи добавляются в $PLAN -----------

plan_builds() {
  tidy_plan_builds "$ROOT" >>"$PLAN"
}

# --- показ ---------------------------------------------------------------------------

show_category() {
  awk -F '\t' -v cat="$1" -v root="$ROOT/" '
    function human(kb) {
      if (kb >= 1024) return sprintf("%.1f МБ", kb / 1024)
      return sprintf("%d КиБ", kb)
    }
    BEGIN {
      word["rm"] = "удалить"; word["thin"] = "утончить"; word["mv"] = "перенести"
      print "== " cat " =="
    }
    $2 == cat {
      p = $3
      if (index(p, root) == 1) p = substr(p, length(root) + 1)
      if ($1 == "skip") { printf "пропуск  %s  — %s\n", p, $5; sk++; next }
      w = ($1 in word) ? word[$1] : $1
      line = w "  " p "  " human($4)
      if ($5 != "") line = line "  (" $5 ")"
      print line
      n++; kb += $4
    }
    END { printf "-- %s: действий %d, пропусков %d, ≈ %.1f МБ\n", cat, n, sk, kb / 1024 }' "$PLAN"
}

show_plan() {
  printf '%s\n' "$CATEGORIES" | tr '|' '\n' | while IFS= read -r _cat; do
    [ -n "$_cat" ] || continue
    show_category "$_cat"
  done
  awk -F '\t' '$1 != "skip" { n++; kb += $4 } END { printf "итого: действий %d, ≈ %.1f МБ\n", n, kb / 1024 }' "$PLAN"
}

TIDY_SIZES=1
export TIDY_SIZES

plan_builds

show_plan

if [ "$APPLY" != 1 ]; then
  echo "режим показа: ничего не удалено; удалить — make tidy APPLY=1"
  exit 0
fi

_rc=0
tidy_exec "$ROOT" <"$PLAN" || _rc=1
_freed=$(awk -F '\t' '$1 != "skip" { kb += $4 } END { printf "%.1f", kb / 1024 }' "$PLAN")
echo "освобождено ≈ $_freed МБ"
exit "$_rc"
