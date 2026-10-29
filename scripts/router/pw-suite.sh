#!/bin/sh
# scripts/router/pw-suite.sh <id цели> [ядро] [--switch <ядро> | --part read|state] — Playwright против настоящей панели цели.
#
# Запускает frontend/playwright.router.config.ts (один воркер, без повторов): вход в
# панель делает global-setup один раз, тесты работают с настоящим API и ядром. Каждая
# цель идёт своим процессом (run.sh запускает цели параллельно, D-23), у каждой свой
# каталог <отчёт>/<id>/<ядро>/pw/ (трассы, снимки, JSON-отчёт, журнал запуска).
#
# --switch <ядро>: вместо набора запускается только core-switch.spec.ts, который через
# интерфейс панели («Службы») переключает устройство на это ядро (XCP_WANT_CORE, матрица
# ядер, D-20). Артефакты — в <отчёт>/<id>/<ядро>/pw-switch/, результат — строка
# pw:core-switch:<ядро>.
#
# --part read|state (вне --switch): запускается одна часть набора (D-07, D-18). Спеки делятся
# по разметке: спеки с точной строкой `// e2e-kind: read` в первых пяти строках — часть read,
# остальные (без строки вида, умолчание — изменяющий) — часть state. Разметку проверяет
# scripts/check-router-kinds.js: в спеке read нет изменяющих вызовов.
#   read   только спеки с `// e2e-kind: read`, PAGE_WORKERS воркеров, артефакты — <отчёт>/<id>/<ядро>/pw-read/;
#          идёт параллельно окну наблюдения смоука (smoke.sh --observe-only)
#   state  остальные спеки, один воркер, артефакты — <отчёт>/<id>/<ядро>/pw/
# Пустая часть (в выбранных спеках нет подходящих) — выход 0 без строк результата.
# Без --part обе части идут подряд в <отчёт>/<id>/<ядро>/pw/ (так зовёт session.mjs).
#
# Результаты: строки `pw:<название теста>` в <отчёт>/<id>/results.tsv:
#   PASS, FAIL, SKIP, а также KNOWN (тест с меткой knownFailure упал, как ожидалось) и
#   XPASS (помеченный тест прошёл: метка больше не нужна, прогон красный).
# При FAIL и XPASS хвост xcp.log с устройства (200 строк, после rt_redact) кладётся в
# <отчёт>/<id>/<ядро>/xcp-tail.log. Трассы и снимки остаются только в build/router/
# (на них могут быть имена узлов): в PR и todo их не прикладывают. Временные каталоги
# воркеров `.playwright-artifacts-*` удаляются после каждого запуска Playwright и при выходе
# этапа; артефакты упавших тестов (<артефакты>/<тест>/trace.zip, снимки) остаются.
#
# Окружение:
#   RT_SPECS   спеки через пробел (по умолчанию все tests/router/**/*.spec.ts);
#              при --switch не используется
#   RT_PW_GREP регулярное выражение --grep по названиям тестов (набор changed сужает им
#              обход страниц до затронутых маршрутов); при --switch не используется
#   RT_PW_CORE_ONLY=1  только тесты с тегом @core (вторая ветвь набора changed): --grep @core,
#              а вместе с RT_PW_GREP — (?=.*@core) и затем RT_PW_GREP; --pass-with-no-tests;
#              часть без таких тестов завершается кодом 0 без строк результата; при --switch
#              не используется
#   RT_REPORT  каталог отчёта (если не задан, создаётся новый); относительный — от корня
#              рабочей копии, вне build/router — код 2
#   XCP_T_<id>_SLOW  множитель таймаутов цели (в локальном конфиге; по умолчанию 1,
#                    для mipsle и mips — 2)
#   XCP_T_<id>_PAGE_WORKERS  воркеров обхода страниц (по умолчанию 3)
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна, иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

usage() {
  echo "usage: pw-suite.sh <id цели> [ядро] [--switch <ядро> | --part read|state]" >&2
  exit 2
}
[ $# -ge 1 ] || usage
ID=$1
shift
CORE=""
SWITCH=""
PART=""
while [ $# -gt 0 ]; do
  case "$1" in
    --switch)
      [ $# -ge 2 ] || usage
      SWITCH=$2
      case "$SWITCH" in xray | mihomo) ;; *) usage ;; esac
      shift 2
      ;;
    --part)
      [ $# -ge 2 ] || usage
      PART=$2
      case "$PART" in read | state) ;; *) usage ;; esac
      shift 2
      ;;
    *)
      [ -z "$CORE" ] || usage
      CORE=$1
      shift
      ;;
  esac
done
[ "$CORE" != "-" ] || CORE=""
[ -z "$PART" ] || [ -z "$SWITCH" ] || usage

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
rt_report_use
RT_CORE=$CORE
export RT_CORE

ROOT=$(rt_repo_root)
ARCH=$(rt_get "$ID" ARCH)
URL=$(rt_get "$ID" URL)
SLOW=$(rt_get "$ID" SLOW)
# Множитель таймаутов по умолчанию: на mipsle диск и процессор заметно медленнее,
# обход страниц иначе упирается в таймауты загрузки; локальный конфиг может переопределить.
if [ -z "$SLOW" ]; then
  case "$ARCH" in
    mipsle | mips) SLOW=2 ;;
    *) SLOW=1 ;;
  esac
fi

CORE_DIR=${CORE:-unknown}
PW_NAME=pw
SPECS=${RT_SPECS:-}
if [ -n "$SWITCH" ]; then
  PW_NAME=pw-switch
  SPECS=core-switch.spec.ts
fi
[ "$PART" != read ] || PW_NAME=pw-read
PW_DIR="$RT_REPORT/$ID/$CORE_DIR/$PW_NAME"
case "$PW_DIR" in
  "$ROOT"/build/router/*) ;;
  *)
    echo "pw-suite: путь вывода вне build/router: $PW_DIR" >&2
    exit 2
    ;;
esac
mkdir -p "$PW_DIR"
STATE="$PW_DIR/state.json"
LOG="$PW_DIR/run.log"
JSON="$PW_DIR/report.json"
OUT="$PW_DIR/artifacts"
rm -f "$STATE" "$STATE.restore.json" "$JSON" "$PW_DIR/report-pages.json"

# Файл сессии содержит cookie и CSRF: после этапа его на диске быть не должно.
trap 'rm -f "$STATE" "$STATE.restore.json"; rm -rf "$OUT"/.playwright-artifacts-*' EXIT
# Прерывание сигналом тоже запускает EXIT-trap (иначе storageState с cookie остался бы на ПК).
trap 'exit 130' INT TERM HUP

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
# Спеки только чтения (`// e2e-kind: read`) идут в несколько воркеров (PAGE_WORKERS цели, по
# умолчанию 3); остальные меняют состояние устройства и идут одним воркером, после них. Два
# запуска Playwright — два JSON-отчёта, разбор общий.
PAGE_WORKERS=$(rt_get "$ID" PAGE_WORKERS)
[ -n "$PAGE_WORKERS" ] || PAGE_WORKERS=3

# run_pw <json> <воркеров> <спеки...>: один запуск Playwright, журнал дописывается в $LOG
run_pw() {
  _json=$1
  _workers=$2
  shift 2
  _pwrc=0
  (
    cd "$ROOT/frontend"
    # core-switch.spec.ts регистрирует тест только при XCP_WANT_CORE: в обычном наборе
    # переменная не должна просочиться из окружения вызывающего
    unset XCP_WANT_CORE
    [ -z "$SWITCH" ] || export XCP_WANT_CORE=$SWITCH
    # --grep собирается в позиционные параметры: выражение содержит пробелы и скобки.
    # RT_PW_CORE_ONLY=1 (вторая ветвь набора changed) оставляет только тесты с тегом @core, в
    # том числе внутри сужения RT_PW_GREP: то же выражение строит select-changed.mjs для
    # RT_SPECS_CORE. Спек без таких тестов — не ошибка (--pass-with-no-tests).
    _grep=${RT_PW_GREP:-}
    if [ "${RT_PW_CORE_ONLY:-}" = 1 ] && [ -z "$SWITCH" ]; then
      if [ -n "$_grep" ]; then _grep="(?=.*@core)$_grep"; else _grep="@core"; fi
      set -- "$@" --pass-with-no-tests
    fi
    [ -n "$_grep" ] && [ -z "$SWITCH" ] && set -- "$@" --grep "$_grep"
    XCP_URL=$URL \
      XCP_PASSWORD=$(rt_get "$ID" PASSWORD) \
      XCP_ARCH=$ARCH \
      XCP_CORE=${CORE:--} \
      XCP_SLOW=$SLOW \
      XCP_PW_STATE=$STATE \
      XCP_PW_OUT=$OUT \
      XCP_PW_JSON=$_json \
      XCP_PW_WORKERS=$_workers \
      "$PWBIN" test -c playwright.router.config.ts "$@"
  ) >>"$LOG" 2>&1 || _pwrc=$?
  # временные каталоги воркеров Playwright — только сироты после выхода процесса
  rm -rf "$OUT"/.playwright-artifacts-*
  return "$_pwrc"
}

READ_SPECS=""
REST_SPECS=""
if [ -n "$SWITCH" ]; then
  REST_SPECS=$SPECS
else
  [ -n "$SPECS" ] || SPECS=$(cd "$ROOT/frontend/tests/router" && ls ./*.spec.ts | sed 's#^\./##' | tr '\n' ' ')
  for _s in $SPECS; do
    # вид спека — строка `// e2e-kind: read` в первых пяти строках; без неё спек изменяющий.
    # select-changed.mjs и session.mjs передают пути от frontend/ (`tests/router/x.spec.ts`),
    # полный набор — имена файлов: путь без каталога достраивается.
    case "$_s" in
      */*) _f="$ROOT/frontend/$_s" ;;
      *) _f="$ROOT/frontend/tests/router/$_s" ;;
    esac
    if [ -f "$_f" ] && head -n 5 "$_f" | grep -qx '// e2e-kind: read'; then
      READ_SPECS="$READ_SPECS $_s"
    else
      REST_SPECS="$REST_SPECS $_s"
    fi
  done
fi

# --part: остаётся только своя часть набора
case "$PART" in
  read) REST_SPECS="" ;;
  state) READ_SPECS="" ;;
esac
if [ -n "$PART" ] && [ -z "$(echo $READ_SPECS)" ] && [ -z "$(echo $REST_SPECS)" ]; then
  # в выбранных спеках нет подходящих этой части: нет ни строк результата, ни FAIL pw:run
  exit 0
fi

: >"$LOG"
rc=0
JSONS=""
if [ -n "$(echo $READ_SPECS)" ]; then
  JSONS="$PW_DIR/report-pages.json"
  rm -f "$PW_DIR/report-pages.json"
  # shellcheck disable=SC2086
  run_pw "$PW_DIR/report-pages.json" "$PAGE_WORKERS" $READ_SPECS || rc=$?
fi
if [ -n "$(echo $REST_SPECS)" ]; then
  JSONS="$JSONS $JSON"
  # shellcheck disable=SC2086
  run_pw "$JSON" 1 $REST_SPECS || { _r=$?; [ "$rc" != 0 ] || rc=$_r; }
fi
rt_redact <"$LOG" >"$LOG.red" && mv "$LOG.red" "$LOG"

# --- разбор --------------------------------------------------------------------
TAB=$(printf '\t')
BAD=0
found=0
: >"$PW_DIR/parsed.tsv"
for _j in $JSONS; do
  [ -f "$_j" ] || continue
  node "$SCRIPT_DIR/pw-parse.js" "$_j" >>"$PW_DIR/parsed.tsv" || true
done
while IFS="$TAB" read -r st chk det || [ -n "$st" ]; do
  [ -n "$st" ] || continue
  found=$((found + 1))
  rt_result "$ID" "$st" "pw:$chk" "$det"
  case "$st" in FAIL | XPASS) BAD=$((BAD + 1)) ;; esac
done <"$PW_DIR/parsed.tsv"

if [ "$found" = 0 ] && [ "$rc" = 0 ] && [ "${RT_PW_CORE_ONLY:-}" = 1 ] && [ -z "$SWITCH" ]; then
  # вторая ветвь: в части нет ни одного теста с тегом @core — строк результата нет и FAIL pw:run тоже
  :
elif [ "$found" = 0 ]; then
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
