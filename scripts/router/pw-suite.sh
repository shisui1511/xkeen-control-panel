#!/bin/sh
# scripts/router/pw-suite.sh <id цели> <ядро> — Playwright против настоящей панели цели.
#
# Запускает frontend/playwright.router.config.ts (один воркер, без повторов): вход в
# панель делает global-setup один раз, тесты работают с настоящим API и ядром. Каждая
# цель идёт своим процессом (run.sh запускает цели параллельно, D-23), у каждой свой
# каталог <отчёт>/<id>/<ядро>/pw/ (трассы, снимки, JSON-отчёт, журнал запуска).
#
# Результаты: строки `pw:<название теста>` в <отчёт>/<id>/results.tsv:
#   PASS, FAIL, SKIP, а также KNOWN (тест с меткой knownFailure упал, как ожидалось) и
#   XPASS (помеченный тест прошёл: метка больше не нужна, прогон красный).
# При FAIL и XPASS хвост xcp.log с устройства (200 строк, после rt_redact) кладётся в
# <отчёт>/<id>/<ядро>/xcp-tail.log. Трассы и снимки остаются только в build/router/
# (на них могут быть имена узлов): в PR и todo их не прикладывают.
#
# Окружение:
#   RT_SPECS   спеки через пробел (по умолчанию все tests/router/**/*.spec.ts)
#   RT_REPORT  каталог отчёта (если не задан, создаётся новый)
#   XCP_T_<id>_SLOW  множитель таймаутов цели (в локальном конфиге; по умолчанию 1)
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна, иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

[ $# -ge 1 ] || {
  echo "usage: pw-suite.sh <id цели> [ядро]" >&2
  exit 2
}
ID=$1
CORE=${2:-}
[ "$CORE" != "-" ] || CORE=""

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
[ -n "${RT_REPORT:-}" ] || rt_report_init
RT_CORE=$CORE
export RT_CORE

ROOT=$(rt_repo_root)
ARCH=$(rt_get "$ID" ARCH)
URL=$(rt_get "$ID" URL)
SLOW=$(rt_get "$ID" SLOW)
[ -n "$SLOW" ] || SLOW=1

CORE_DIR=${CORE:-unknown}
PW_DIR="$RT_REPORT/$ID/$CORE_DIR/pw"
mkdir -p "$PW_DIR"
STATE="$PW_DIR/state.json"
LOG="$PW_DIR/run.log"
JSON="$PW_DIR/report.json"
OUT="$PW_DIR/artifacts"
rm -f "$STATE" "$JSON"

# Файл сессии содержит cookie и CSRF: после этапа его на диске быть не должно.
trap 'rm -f "$STATE"' EXIT

PWBIN="$ROOT/frontend/node_modules/.bin/playwright"
if [ ! -x "$PWBIN" ]; then
  echo "pw-suite: нет $PWBIN — npm ci" >&2
  npm --prefix "$ROOT/frontend" ci >"$PW_DIR/npm-ci.log" 2>&1 || {
    rt_result "$ID" FAIL "pw:setup" "npm ci в frontend не удался: $(tail -n 2 "$PW_DIR/npm-ci.log" | tr '\n' ' ')"
    exit 1
  }
fi

# --- запуск --------------------------------------------------------------------
# Пароль идёт только в окружение процесса Playwright; в отчёт и журнал не попадает.
rc=0
(
  cd "$ROOT/frontend"
  XCP_URL=$URL \
    XCP_PASSWORD=$(rt_get "$ID" PASSWORD) \
    XCP_ARCH=$ARCH \
    XCP_CORE=${CORE:--} \
    XCP_SLOW=$SLOW \
    XCP_PW_STATE=$STATE \
    XCP_PW_OUT=$OUT \
    XCP_PW_JSON=$JSON \
    "$PWBIN" test -c playwright.router.config.ts ${RT_SPECS:-}
) >"$LOG" 2>&1 || rc=$?
rt_redact <"$LOG" >"$LOG.red" && mv "$LOG.red" "$LOG"

# --- разбор --------------------------------------------------------------------
TAB=$(printf '\t')
BAD=0
found=0
if [ -f "$JSON" ]; then
  node "$SCRIPT_DIR/pw-parse.js" "$JSON" >"$PW_DIR/parsed.tsv" || true
  while IFS="$TAB" read -r st chk det || [ -n "$st" ]; do
    [ -n "$st" ] || continue
    found=$((found + 1))
    rt_result "$ID" "$st" "pw:$chk" "$det"
    case "$st" in FAIL | XPASS) BAD=$((BAD + 1)) ;; esac
  done <"$PW_DIR/parsed.tsv"
fi

if [ "$found" = 0 ]; then
  # отчёта нет или он пуст: причина — в журнале запуска (панель не отвечает и т. п.)
  reason=$(grep -m 1 -E 'панель не отвечает|вход в панель не удался|не заданы переменные' "$LOG" || true)
  [ -n "$reason" ] || reason="Playwright завершился с кодом $rc без результатов: $(tail -n 3 "$LOG" | tr '\n' ' ')"
  rt_result "$ID" FAIL "pw:run" "$reason"
  BAD=$((BAD + 1))
elif [ "$rc" != 0 ] && [ "$BAD" = 0 ]; then
  rt_result "$ID" FAIL "pw:run" "Playwright завершился с кодом $rc без упавших тестов"
  BAD=$((BAD + 1))
fi

# --- артефакты падения (D-16): хвост xcp.log ---------------------------------------
if [ "$BAD" != 0 ]; then
  rt_ssh "$ID" "tail -n 200 /opt/var/log/xcp.log" 2>/dev/null | rt_redact >"$RT_REPORT/$ID/$CORE_DIR/xcp-tail.log" || true
  [ -s "$RT_REPORT/$ID/$CORE_DIR/xcp-tail.log" ] || echo "хвост xcp.log недоступен" >"$RT_REPORT/$ID/$CORE_DIR/xcp-tail.log"
fi

[ "$BAD" = 0 ] || exit 1
exit 0
