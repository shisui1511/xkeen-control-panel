#!/bin/sh
# scripts/tidy.sh — уборка за агентами на ПК: сборки, отчёты стенда, кэш релизов.
#
#   scripts/tidy.sh                  показ: что будет удалено и сколько места, ничего не меняется
#   scripts/tidy.sh --apply          выполнить ровно показанный план (make tidy APPLY=1)
#   scripts/tidy.sh --drop-legacy    вместо ротации убрать весь старый формат build/router одним пунктом
#   scripts/tidy.sh --rotate-builds  только ротация сборок build/xcp_v*_<арх> (вызывает Makefile)
#   scripts/tidy.sh -h | --help      эта справка
#
# Пороги — переменные XCP_TIDY_* (умолчания и смысл — в шапке scripts/tidy-lib.sh).
# Показ и исполнение строятся по одному плану: по категориям, с действием, путём,
# размером и пропущенным с причиной. Пути — от корня репозитория.
# Отчёты, кэш релизов и старый формат build/router трогаются, только пока общая
# блокировка стенда свободна (RT_LOCK_HELD=1 — её держит родительский прогон);
# иначе эти категории пропускаются с причиной «блокировка занята прогоном».
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
DROP_LEGACY=0
for _a in "$@"; do
  case "$_a" in
    --apply) APPLY=1 ;;
    --rotate-builds) ROTATE_BUILDS=1 ;;
    --drop-legacy) DROP_LEGACY=1 ;;
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
CATEGORIES="сборки|отчёты|кэш релизов|старый формат build/router"

# Общая блокировка стенда (Pitfall: в потомке run.sh fd 9 уже держит родитель, пробовать
# заново нельзя). Занята чужим прогоном — отчёты, кэш релизов и старый формат пропускаются.
LOCK_BUSY=0
if [ "${RT_LOCK_HELD:-0}" != 1 ]; then
  if command -v flock >/dev/null 2>&1; then
    exec 9>"$(rt_common_dir)/router-test.lock"
    if ! flock -n 9; then
      LOCK_BUSY=1
    fi
  else
    LOCK_BUSY=1
  fi
fi

# --- построение плана: одна функция на категорию, записи добавляются в $PLAN -----------

plan_builds() {
  tidy_plan_builds "$ROOT" >>"$PLAN"
}

plan_locked() { # <категория> <функция плана…>
  _pl_cat=$1
  shift
  if [ "$LOCK_BUSY" = 1 ]; then
    printf 'skip\t%s\t%s\t0\t%s\n' "$_pl_cat" "$ROOT/build/router" "блокировка занята прогоном" >>"$PLAN"
  else
    "$@" >>"$PLAN"
  fi
}

plan_reports() {
  plan_locked "отчёты" tidy_plan_reports "$ROOT"
}

plan_releases() {
  plan_locked "кэш релизов" tidy_plan_releases "$ROOT"
}

plan_legacy() {
  if [ "$DROP_LEGACY" = 1 ]; then
    plan_locked "старый формат build/router" tidy_plan_legacy_all "$ROOT"
  else
    plan_locked "старый формат build/router" tidy_plan_legacy "$ROOT"
  fi
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
plan_reports
plan_releases
plan_legacy

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
