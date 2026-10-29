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
CATEGORIES="сборки|отчёты|кэш релизов|старый формат build/router|обрывки frontend/build/router|корневые хвосты"

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

# Обрывки frontend/build/router/<имя>: Playwright разрешил относительный путь отчёта от
# frontend/. Обрывок с одноимённым отчётом build/router/<имя> и без общих файлов
# переносится в отчёт, без отчёта удаляется, с общими файлами пропускается. state.json
# (cookie и CSRF закрытых сессий) удаляются, а не переносятся.
plan_stubs() {
  _ps_cat="обрывки frontend/build/router"
  _ps_dir="$ROOT/frontend/build/router"
  _ps_skipped=0
  [ -d "$_ps_dir" ] || return 0
  _ps_a=$(mktemp)
  _ps_b=$(mktemp)
  _ps_names=$(mktemp)
  LC_ALL=C ls -1A "$_ps_dir" >"$_ps_names"
  while IFS= read -r _ps_n; do
    _ps_stub="$_ps_dir/$_ps_n"
    if [ ! -d "$_ps_stub" ] || [ -L "$_ps_stub" ]; then
      _ps_skipped=1
      printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "не каталог отчёта" >>"$PLAN"
      continue
    fi
    _ps_rep="$ROOT/build/router/$_ps_n"
    if [ -d "$_ps_rep" ] && [ ! -L "$_ps_rep" ]; then
      find "$_ps_stub" -type f \( -name state.json -o -name state.json.restore.json \) |
        _tl_emit_paths rm "$_ps_cat" "сессия панели закрыта: cookie и CSRF не переносятся" >>"$PLAN"
      (cd "$_ps_stub" && find . -type f ! -name state.json ! -name state.json.restore.json | LC_ALL=C sort) >"$_ps_a"
      (cd "$_ps_rep" && find . -type f | LC_ALL=C sort) >"$_ps_b"
      if [ -n "$(comm -12 "$_ps_a" "$_ps_b")" ]; then
        _ps_skipped=1
        printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "файлы пересекаются" >>"$PLAN"
      else
        printf 'mv\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "$_ps_rep" >>"$PLAN"
      fi
    else
      printf '%s\n' "$_ps_stub" | _tl_emit_paths rm "$_ps_cat" "нет одноимённого отчёта" >>"$PLAN"
    fi
  done <"$_ps_names"
  rm -f "$_ps_a" "$_ps_b" "$_ps_names"
  # остаток frontend/build — после обрывков
  if [ "$_ps_skipped" = 1 ]; then
    printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$ROOT/frontend/build" "остались пропущенные обрывки" >>"$PLAN"
  else
    # sweep убирает только пустые каталоги: если перенос обрывка не удался, его файлы не пропадут
    printf 'sweep\t%s\t%s\t0\t%s\n' "$_ps_cat" "$ROOT/frontend/build" "остаток после обрывков" >>"$PLAN"
  fi
}

# Корневые хвосты (D-21): сборки тестов и покрытия, кэши, выводы Playwright. Удаляется только то,
# что git не отслеживает.
plan_tails() {
  _pt_cat="корневые хвосты"
  _pt_list=$(mktemp)
  {
    [ -f "$ROOT/xcp" ] && [ ! -L "$ROOT/xcp" ] && printf '%s\n' "$ROOT/xcp"
    for _pt_f in "$ROOT"/*.test "$ROOT"/*.out; do
      if [ -f "$_pt_f" ] && [ ! -L "$_pt_f" ]; then
        printf '%s\n' "$_pt_f"
      fi
    done
    for _pt_f in node_modules commands.md .go frontend/playwright-report frontend/test-results \
      frontend/test-results-router frontend/test-results-router.json build/xcp; do
      if [ -e "$ROOT/$_pt_f" ] || [ -L "$ROOT/$_pt_f" ]; then
        printf '%s\n' "$ROOT/$_pt_f"
      fi
    done
    for _pt_f in "$ROOT"/build/xcp_test_* "$ROOT"/build/*.log; do
      if [ -e "$_pt_f" ] || [ -L "$_pt_f" ]; then
        printf '%s\n' "$_pt_f"
      fi
    done
  } >"$_pt_list" || true
  while IFS= read -r _pt_p; do
    _pt_rel=${_pt_p#"$ROOT"/}
    if [ -n "$(git ls-files -- "$_pt_rel")" ]; then
      printf 'skip\t%s\t%s\t0\t%s\n' "$_pt_cat" "$_pt_p" "отслеживается git" >>"$PLAN"
    else
      printf '%s\n' "$_pt_p" | _tl_emit_paths rm "$_pt_cat" "" >>"$PLAN"
    fi
  done <"$_pt_list"
  rm -f "$_pt_list"
}

# Перенос обрывка в отчёт (действие mv плана): tidy_exec отдаёт его сюда.
tidy_exec_hook() { # <действие> <категория> <путь> <КиБ> <примечание>
  case "$1" in
    mv)
      case "$5" in
        "$ROOT"/build/router/?*) ;;
        *) return 1 ;;
      esac
      [ -d "$5" ] || return 1
      cp -Rp "$3/." "$5/" || return 1
      tidy_safe_rm "$ROOT" "$3"
      ;;
    sweep)
      [ "$3" = "$ROOT/frontend/build" ] || return 1
      [ -d "$3" ] && [ ! -L "$3" ] || return 0
      find "$3" -depth -type d -empty -delete
      ;;
    *) return 1 ;;
  esac
}

# --- показ ---------------------------------------------------------------------------

show_category() {
  awk -F '\t' -v cat="$1" -v root="$ROOT/" '
    function human(kb) {
      if (kb >= 1024) return sprintf("%.1f МБ", kb / 1024)
      return sprintf("%d КиБ", kb)
    }
    BEGIN {
      word["rm"] = "удалить"; word["thin"] = "утончить"; word["mv"] = "перенести"; word["sweep"] = "убрать пустые"
      print "== " cat " =="
    }
    $2 == cat {
      p = $3
      if (index(p, root) == 1) p = substr(p, length(root) + 1)
      if ($1 == "skip") { printf "пропуск  %s  — %s\n", p, $5; sk++; next }
      w = ($1 in word) ? word[$1] : $1
      line = w "  " p "  " human($4)
      nt = $5
      if (index(nt, root) == 1) nt = "-> " substr(nt, length(root) + 1)
      if (nt != "") line = line "  (" nt ")"
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
plan_stubs
plan_tails

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
