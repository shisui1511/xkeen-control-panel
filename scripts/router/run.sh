#!/bin/sh
# scripts/router/run.sh [SUITE] — проверка на роутерах (make router-test SUITE=...).
#
# Наборы: smoke — деплой на цели и смоук. Остальные наборы появятся позже.
#
# Порядок: цели из локального конфига -> блокировка на весь прогон -> отчёт ->
# проверка связи -> deploy.sh -> smoke.sh по целям параллельно -> report.md и
# summary.md в build/router/<id>/ (ссылка build/router/last).
#
# Окружение:
#   ROUTERS=<id,id>      выбор целей (по умолчанию все из конфига)
#   RELEASE=<тег>        ставить артефакт GitHub Release вместо сборки ветки
#   RT_HOLD_MINUTES=<N>  после прогона держать блокировку N минут для ручной
#                        проверки; снять раньше: touch <git-common-dir>/router-test.release
#   RT_LOCK_WAIT=<с>     ожидание очереди на роутеры (по умолчанию 3600)
#
# Код выхода: 0 — нет строк FAIL и XPASS, 2 — неизвестный набор, иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

SUITE=${1:-}
case "$SUITE" in
  smoke) ;;
  *)
    echo "router-test: неизвестный набор '${SUITE}'. Поддерживается: smoke" >&2
    echo "router-test: пример: make router-test SUITE=smoke" >&2
    exit 2
    ;;
esac

cd "$(rt_repo_root)"
rt_load_targets
rt_lock
rt_report_init
echo "router-test: отчёт $RT_REPORT"

# --- проверка связи -----------------------------------------------------------

REACHABLE=""
for t in $RT_TARGETS; do
  mkdir -p "$RT_REPORT/$t"
  rc=0
  rt_stage "$t" reachable rt_ssh "$t" "true" || rc=$?
  if [ "$rc" = 0 ]; then
    REACHABLE="$REACHABLE $t"
  else
    rt_result "$t" FAIL reachable "нет связи"
    for c in version xcp-pid api-version; do
      rt_result "$t" SKIP "$c" "нет связи"
    done
  fi
done

# --- деплой ---------------------------------------------------------------------

DEPLOY_ARGS=""
if [ -n "${RELEASE:-}" ]; then
  DEPLOY_ARGS="--release $RELEASE"
fi
EXPECT=""
if [ -n "$REACHABLE" ]; then
  rc=0
  # shellcheck disable=SC2086
  ROUTERS=$(printf '%s' "$REACHABLE" | sed 's/^ //; s/ /,/g') sh "$SCRIPT_DIR/deploy.sh" $DEPLOY_ARGS || rc=$?
  [ -f "$RT_REPORT/version" ] && EXPECT=$(cat "$RT_REPORT/version")
  for t in $REACHABLE; do
    drc=1
    [ -f "$RT_REPORT/$t/deploy.rc" ] && drc=$(cat "$RT_REPORT/$t/deploy.rc")
    if [ "$drc" = 0 ]; then
      DEPLOYED="${DEPLOYED:-} $t"
    elif [ "$drc" = 3 ]; then
      rt_result "$t" FAIL reachable "нет связи"
      for c in version xcp-pid api-version; do
        rt_result "$t" SKIP "$c" "нет связи"
      done
    else
      rt_result "$t" FAIL deploy "деплой завершился с кодом $drc"
      for c in version xcp-pid api-version; do
        rt_result "$t" SKIP "$c" "деплой не выполнен"
      done
    fi
  done
fi

# --- смоук параллельно ------------------------------------------------------------

PIDS=""
for t in ${DEPLOYED:-}; do
  sh "$SCRIPT_DIR/smoke.sh" "$t" "$EXPECT" &
  PIDS="$PIDS $!"
done
for p in $PIDS; do
  wait "$p" || true
done

# --- отчёт -------------------------------------------------------------------------

SHA=$(git rev-parse HEAD)
DIRTY=$(sed -n 's/^dirty=//p' "$RT_REPORT/meta")
FAILS=0
XPASSES=0

{
  echo "# Прогон проверки на роутерах"
  echo
  echo "- Набор: $SUITE"
  echo "- SHA: $SHA (незакоммиченные правки: $DIRTY)"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo
  for t in $RT_TARGETS; do
    arch=$(rt_get "$t" ARCH)
    ver=""
    [ -f "$RT_REPORT/$t/xcp_version" ] && ver=$(cat "$RT_REPORT/$t/xcp_version")
    echo "## Цель $t ($arch)"
    echo
    echo "- Версия xcp: ${ver:--}"
    if [ -f "$RT_REPORT/$t/results.tsv" ]; then
      awk -F'\t' '{c[$1]++} END {printf "- PASS: %d, FAIL: %d, KNOWN: %d, XPASS: %d, SKIP: %d\n", c["PASS"], c["FAIL"], c["KNOWN"], c["XPASS"], c["SKIP"]}' "$RT_REPORT/$t/results.tsv"
    else
      echo "- результатов нет"
    fi
    echo
    echo "Время этапов (секунды, код):"
    echo
    if [ -f "$RT_REPORT/timings.tsv" ]; then
      awk -F'\t' -v t="$t" '$1 == t || $1 == "all" {printf "- %s: %s с (код %s)\n", $2, $3, $4}' "$RT_REPORT/timings.tsv"
    fi
    echo
    if [ -f "$RT_REPORT/$t/results.tsv" ] && awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" {f = 1} END {exit !f}' "$RT_REPORT/$t/results.tsv"; then
      echo "Падения:"
      echo
      awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" {printf "- %s %s: %s\n", $1, $3, $4}' "$RT_REPORT/$t/results.tsv"
      echo
    fi
  done
} >"$RT_REPORT/report.md"

{
  echo "# Сводка прогона"
  echo
  echo "- Набор: $SUITE"
  echo "- SHA: $SHA"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo "- Проверены цели: $(printf '%s' "$RT_TARGETS" | tr ' ' ',')"
  echo
  for t in $RT_TARGETS; do
    arch=$(rt_get "$t" ARCH)
    ver=""
    [ -f "$RT_REPORT/$t/xcp_version" ] && ver=$(cat "$RT_REPORT/$t/xcp_version")
    printf '%s: xcp %s, ' "$arch" "${ver:--}"
    if [ -f "$RT_REPORT/$t/results.tsv" ]; then
      awk -F'\t' '{c[$1]++} END {printf "passed %d, failed %d, known %d, xpass %d, skipped %d\n", c["PASS"], c["FAIL"], c["KNOWN"], c["XPASS"], c["SKIP"]}' "$RT_REPORT/$t/results.tsv"
    else
      echo "результатов нет"
    fi
  done
} >"$RT_REPORT/summary.md"

for t in $RT_TARGETS; do
  if [ -f "$RT_REPORT/$t/results.tsv" ]; then
    f=$(awk -F'\t' '$1 == "FAIL" {n++} END {print n + 0}' "$RT_REPORT/$t/results.tsv")
    x=$(awk -F'\t' '$1 == "XPASS" {n++} END {print n + 0}' "$RT_REPORT/$t/results.tsv")
    FAILS=$((FAILS + f))
    XPASSES=$((XPASSES + x))
  else
    FAILS=$((FAILS + 1))
  fi
done

echo
cat "$RT_REPORT/summary.md"

# --- удержание блокировки для ручной проверки ---------------------------------------

if [ -n "${RT_HOLD_MINUTES:-}" ] && [ "$RT_HOLD_MINUTES" -gt 0 ] 2>/dev/null; then
  RELEASE_FILE="$(rt_common_dir)/router-test.release"
  rm -f "$RELEASE_FILE"
  trap 'rm -f "$RELEASE_FILE"' EXIT INT TERM
  echo "router-test: блокировка удерживается $RT_HOLD_MINUTES мин; снять раньше: touch $RELEASE_FILE"
  END=$(($(date +%s) + RT_HOLD_MINUTES * 60))
  while [ "$(date +%s)" -lt "$END" ] && [ ! -e "$RELEASE_FILE" ]; do
    sleep 1
  done
  rm -f "$RELEASE_FILE"
fi

if [ "$FAILS" -gt 0 ] || [ "$XPASSES" -gt 0 ]; then
  echo "router-test: красный (FAIL: $FAILS, XPASS: $XPASSES)" >&2
  exit 1
fi
echo "router-test: зелёный"
