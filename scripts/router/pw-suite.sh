#!/bin/sh
# scripts/router/pw-suite.sh <id цели> [ядро] [--switch <ядро> | --part read|state] — Playwright против настоящей панели цели.
#
# Запускает frontend/playwright.router.config.ts (без повторов): вход в панель делает
# global-setup один раз на запуск, тесты работают с настоящим API и ядром. Каждая цель идёт
# своим процессом (run.sh запускает цели параллельно, D-23), у каждого запуска Playwright свой
# каталог <отчёт>/<id>/<ядро>/<часть>/ (трассы, снимки, JSON-отчёт, журнал запуска, state.json).
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
#   read   только спеки с `// e2e-kind: read`, PAGE_WORKERS воркеров, артефакты — <отчёт>/<id>/<ядро>/pw-read/
#          (JSON-отчёт report-pages.json); идёт параллельно окну наблюдения смоука (smoke.sh --observe-only)
#   state  остальные спеки, один воркер, артефакты — <отчёт>/<id>/<ядро>/pw/ (report.json)
# Пустая часть (в выбранных спеках нет подходящих) — выход 0 без строк результата.
# Без --part (так зовёт session.mjs) обе части идут подряд, каждая своим запуском и в свой каталог,
# как у run.sh с --part: часть read — в pw-read, часть state — в pw. Пустая часть пропускается;
# пустые обе — строка FAIL pw:run (журнал — pw/run.log). Раньше обе части писали в pw/, и второй
# запуск Playwright стирал артефакты первого (он очищает outputDir при старте).
#
# Результаты: строки `pw:<название теста>` в <отчёт>/<id>/results.tsv (или RT_RESULTS_NAME):
#   PASS, FAIL, SKIP, а также KNOWN (тест с меткой knownFailure упал, как ожидалось),
#   XPASS (помеченный тест прошёл: метка больше не нужна, прогон красный) и QFAIL/QPASS
#   (тест в карантине: в итог этапа не входит). Разбор и проверки — по каждому запуску:
#   запуск без результатов — FAIL pw:run с причиной из его журнала; ненулевой код без упавших
#   тестов этого запуска (BAD=0 и QFAILS=0) — FAIL pw:run.
# При FAIL и XPASS хвост xcp.log с устройства (200 строк, после rt_redact) кладётся в
# <отчёт>/<id>/<ядро>/[<RT_ART_SUB>/]xcp-tail.log. Трассы и снимки остаются только в build/router/
# (на них могут быть имена узлов): в PR и todo их не прикладывают. Временные каталоги
# воркеров `.playwright-artifacts-*` удаляются после каждого запуска Playwright и при выходе
# этапа; артефакты упавших тестов (<артефакты>/<тест>/trace.zip, снимки) остаются.
#
# Потребители файлов этапа: results.tsv (или RT_RESULTS_NAME) — run.sh (итог и сводка),
# branch-report.mjs, session.mjs (ожидания прохода; строки pw:run и pw:setup — служебные);
# <ядро>/pw*/report*.json — branch-report.mjs (самые долгие тесты, теги ядра): pw-read/report-pages.json,
# pw/report.json, pw-2/report.json и pw-switch/report.json под этот обход попадают; run.log,
# parsed.tsv и artifacts/ читает только человек. Каталоги в <ядро>/<RT_ART_SUB>/ обход
# branch-report.mjs не видит: подкаталог читает тот, кто его задал.
#
# Окружение:
#   RT_SPECS   спеки через пробел (по умолчанию все tests/router/**/*.spec.ts);
#              при --switch не используется
#   RT_SPECS_FIRST  подмножество спеков части state, которое идёт первым запуском (каталог pw);
#              остальные спеки части — вторым запуском в pw-2 (затронутые спеки гейта — первыми).
#              Спек сравнивается без префикса tests/router/. Пересечение пусто — часть идёт одним
#              запуском в pw; часть read и --switch его не читают
#   RT_PW_GREP регулярное выражение --grep по названиям тестов (набор changed сужает им
#              выбор до затронутых маршрутов); при --switch не используется
#   RT_PW_GREP_INVERT  регулярное выражение --grep-invert (отсев яруса full тегом, D-14) во всех
#              запусках, кроме --switch: одним аргументом, без разбиения оболочкой; перевод строки
#              и одинарная кавычка — отказ с кодом 2. С ним --pass-with-no-tests: запуск, где отсев
#              убрал все тесты, завершается кодом 0 без строк результата
#   RT_PW_CORE_ONLY=1  только тесты с тегом @core (вторая ветвь набора changed): --grep @core,
#              а вместе с RT_PW_GREP — (?=.*@core) и затем RT_PW_GREP; --pass-with-no-tests;
#              часть без таких тестов завершается кодом 0 без строк результата; при --switch
#              не используется
#   RT_ART_SUB подкаталог артефактов ветви ([a-z0-9-]+, иначе выход 2): каталоги частей и
#              xcp-tail.log — в <отчёт>/<id>/<ядро>/<RT_ART_SUB>/ (перепрогон в тот же отчёт)
#   RT_RESULTS_NAME  имя файла результатов вместо results.tsv (lib.sh, rt_result)
#   RT_REPORT  каталог отчёта (если не задан, создаётся новый); относительный — от корня
#              рабочей копии, вне build/router — код 2
#   XCP_T_<id>_SLOW  множитель таймаутов цели (в локальном конфиге; по умолчанию 1,
#                    для mipsle и mips — 2)
#   XCP_T_<id>_PAGE_WORKERS  воркеров части read (по умолчанию 3)
#
# Сверка версии стенда на границе спека (D-13, RTR-15). Каждому запуску Playwright передаются
# XCP_PW_VERSION_GUARD=<id цели> (подключает репортёр frontend/tests/router/lib/version-reporter.ts),
# XCP_PW_GUARD_SCRIPT=<абсолютный путь scripts/router/version-guard.sh>,
# XCP_PW_OK_SPECS=<каталог запуска>/version-ok-specs.txt (спеки, прошедшие границу, по строке
# `tests/router/<спек>`) и XCP_PW_STOPPED=<каталог запуска>/version-stopped (признак остановки:
# граница, на которой сменилась версия; пишется до сигнала) и
# XCP_PW_GUARD_ERRORS=<каталог запуска>/version-guard-errors (границы, где сама сверка не
# выполнилась: код version-guard.sh не 0 и 4, тайм-аут или сигнал; строка `<граница><TAB><код>`;
# код 3 — нет связи: finish_run повторяет сверку границы, четвёртый исход — FAIL version-guard-error).
# Файлы сбрасываются при выборе каталога запуска; каждая строка version-guard-errors — FAIL
# version-guard-error в finish_run (как в go-suite.sh; это не смена версии: класс new), сверка не
# отключается молча. После каждого спека репортёр зовёт version-guard.sh; при смене версии он
# пишет version-stopped и шлёт процессу Playwright SIGINT (мягкая остановка). Тогда этап
# не начинает следующие запуски (часть state после read, pw-2), дописывает их спеки строками
# `notrun<TAB><ядро><TAB>Playwright: tests/router/<спек>` в <отчёт>/<id>/version-stop.tsv,
# собирает хвост xcp.log и выходит с кодом 4. Разбор остановленного запуска (finish_run):
# прерванные остановкой тесты — не FAIL, а строки `notrun … (прервано остановкой, тестов N)`;
# FAIL и XPASS спеков, не прошедших границу, — ещё и строки `suspect<TAB><ядро><TAB>pw:<проверка>`
# (класс stand); падения спеков из version-ok-specs.txt — обычные. Служебные файлы запуска:
# files.tsv (проверка → спек, pw-parse.js --files-out) и interrupted.tsv (--interrupted-out).
# Потребители кода 4 и version-stop.tsv — run.sh
# и branch-report.mjs (остановка цикла ветвей, раздел остановки), session.mjs (остановка
# сессии) и tree-step.sh (шаг bisect при коде 4 — SKIP, а не вердикт коммита); формы строк — контракт сверки версии (rt_version_guard в scripts/router/lib.sh).
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна, 4 — смена версии стенда, этап
# остановлен, иначе 1.

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

# Значения, которые становятся путём или аргументом Playwright (T-148.1-07, T-148.1-08): проверка
# до всякой работы со стендом.
case "${RT_ART_SUB:-}" in
  *[!a-z0-9-]*)
    echo "pw-suite: RT_ART_SUB с недопустимыми символами (только a-z, 0-9 и -)" >&2
    exit 2
    ;;
esac
case "${RT_PW_GREP_INVERT:-}" in
  *"'"* | *"
"*)
    echo "pw-suite: RT_PW_GREP_INVERT с недопустимыми символами (одинарная кавычка или перевод строки)" >&2
    exit 2
    ;;
esac

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
# Каталог ветви: <ядро>[/<RT_ART_SUB>]; каталоги запусков (pw-read, pw, pw-2, pw-switch) — в нём.
SUB=""
[ -z "${RT_ART_SUB:-}" ] || SUB="/$RT_ART_SUB"
SPECS=${RT_SPECS:-}
if [ -n "$SWITCH" ]; then
  SPECS=core-switch.spec.ts
fi

# Каталоги запусков этого этапа: EXIT-trap убирает state.json (cookie и CSRF) только в них —
# соседние этапы той же ветви пишут в другие каталоги.
USED=""
PW_DIR=""
STATE=""
LOG=""
OUT=""

# use_dir <имя>: каталог запуска <ветвь>/<имя> — проверка пути, создание, сброс файлов
# прошлого запуска; задаёт PW_DIR, STATE, LOG, OUT.
use_dir() {
  PW_DIR="$RT_REPORT/$ID/$CORE_DIR$SUB/$1"
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
  OUT="$PW_DIR/artifacts"
  rm -f "$STATE" "$STATE.restore.json" "$PW_DIR/report.json" "$PW_DIR/report-pages.json"
  # файлы сверки версии прошлого запуска: признак остановки удаляется (его наличие — сигнал),
  # список спеков, прошедших границу, обнуляется
  rm -f "$PW_DIR/version-stopped" "$PW_DIR/version-guard-errors"
  : >"$PW_DIR/version-ok-specs.txt"
  case " $USED " in
    *" $1 "*) ;;
    *) USED="$USED $1" ;;
  esac
}

# Файл сессии содержит cookie и CSRF: после этапа его на диске быть не должно.
pw_cleanup() {
  for _n in $USED; do
    rm -f "$RT_REPORT/$ID/$CORE_DIR$SUB/$_n/state.json" "$RT_REPORT/$ID/$CORE_DIR$SUB/$_n/state.json.restore.json"
    rm -rf "$RT_REPORT/$ID/$CORE_DIR$SUB/$_n/artifacts"/.playwright-artifacts-*
  done
}
trap pw_cleanup EXIT
# Прерывание сигналом тоже запускает EXIT-trap (иначе storageState с cookie остался бы на ПК).
trap 'exit 130' INT TERM HUP

# Каталог основного запуска создаётся сразу, как и раньше (в нём и журнал npm ci): pw-switch,
# pw-read у --part read, иначе pw.
if [ -n "$SWITCH" ]; then
  use_dir pw-switch
elif [ "$PART" = read ]; then
  use_dir pw-read
else
  use_dir pw
fi

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
# умолчанию 3); остальные меняют состояние устройства и идут одним воркером, после них.
PAGE_WORKERS=$(rt_get "$ID" PAGE_WORKERS)
[ -n "$PAGE_WORKERS" ] || PAGE_WORKERS=3

# run_pw <json> <воркеров> <спеки...>: один запуск Playwright в каталоге PW_DIR, журнал
# дописывается в $LOG
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
    # отсев яруса full: значение — один аргумент вида --grep-invert=<выражение>, без разбиения
    if [ -n "${RT_PW_GREP_INVERT:-}" ] && [ -z "$SWITCH" ]; then
      set -- "$@" "--grep-invert=$RT_PW_GREP_INVERT" --pass-with-no-tests
    fi
    XCP_URL=$URL \
      XCP_PASSWORD=$(rt_get "$ID" PASSWORD) \
      XCP_ARCH=$ARCH \
      XCP_CORE=${CORE:--} \
      XCP_SLOW=$SLOW \
      XCP_PW_STATE=$STATE \
      XCP_PW_OUT=$OUT \
      XCP_PW_JSON=$_json \
      XCP_PW_WORKERS=$_workers \
      XCP_PW_VERSION_GUARD=$ID \
      XCP_PW_GUARD_SCRIPT="$SCRIPT_DIR/version-guard.sh" \
      XCP_PW_OK_SPECS="$PW_DIR/version-ok-specs.txt" \
      XCP_PW_STOPPED="$PW_DIR/version-stopped" \
      XCP_PW_GUARD_ERRORS="$PW_DIR/version-guard-errors" \
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
READ_SPECS=${READ_SPECS# }
REST_SPECS=${REST_SPECS# }

# --part: остаётся только своя часть набора
case "$PART" in
  read) REST_SPECS="" ;;
  state) READ_SPECS="" ;;
esac
if [ -n "$PART" ] && [ -z "$READ_SPECS" ] && [ -z "$REST_SPECS" ]; then
  # в выбранных спеках нет подходящих этой части: нет ни строк результата, ни FAIL pw:run
  exit 0
fi

# RT_SPECS_FIRST (часть state, не --switch): спеки части, названные в нём, — первый запуск (pw),
# остальные — второй (pw-2). Сравнение — без префикса tests/router/.
FIRST_SPECS=$REST_SPECS
SECOND_SPECS=""
if [ -z "$SWITCH" ] && [ -n "${RT_SPECS_FIRST:-}" ] && [ -n "$REST_SPECS" ]; then
  FIRST_SPECS=""
  for _s in $REST_SPECS; do
    _hit=0
    for _w in $RT_SPECS_FIRST; do
      [ "${_w#tests/router/}" != "${_s#tests/router/}" ] || _hit=1
    done
    if [ "$_hit" = 1 ]; then
      FIRST_SPECS="$FIRST_SPECS $_s"
    else
      SECOND_SPECS="$SECOND_SPECS $_s"
    fi
  done
  FIRST_SPECS=${FIRST_SPECS# }
  SECOND_SPECS=${SECOND_SPECS# }
  # пересечение пусто — вся часть одним запуском в pw
  if [ -z "$FIRST_SPECS" ]; then
    FIRST_SPECS=$SECOND_SPECS
    SECOND_SPECS=""
  fi
fi

TAB=$(printf '\t')
BAD=0

# finish_run <json> <код запуска>: разбор JSON-отчёта запуска в PW_DIR/parsed.tsv, строки
# результатов и проверки запуска. RUN_BAD — FAIL и XPASS запуска, QFAILS — упавшие тесты в
# карантине (в BAD не входят): ненулевой код, объяснённый ими, строки FAIL pw:run не даёт (D-17).
#
# Сверка версии (D-13): разбор пишет files.tsv (проверка → спек, по строке на строку теста
# parsed.tsv, в том же порядке). Если репортёр остановил запуск (есть version-stopped), разбор
# ещё и выносит прерванные и не начатые тесты в interrupted.tsv (в результаты они не идут), а
# после разбора в <отчёт>/<id>/version-stop.tsv дописываются: `suspect<TAB><ядро><TAB>pw:<проверка>`
# — на каждую строку FAIL и XPASS, чей спек не прошёл границу (нет в version-ok-specs.txt:
# падение могло быть на сменённой версии, класс stand); `notrun<TAB><ядро><TAB>Playwright:
# <спек> (прервано остановкой, тестов N)` — на каждый спек из interrupted.tsv. Падения спеков,
# прошедших границу, остаются обычными. Ненулевой код Playwright, объяснённый остановкой,
# строки FAIL pw:run не даёт.
finish_run() {
  _fj=$1
  _frc=$2
  # граница, где связь пропала на сверке (код 3 в version-guard-errors), перепроверяется: 0 — версия
  # совпала; 4 — версия сменилась (признак остановки, дальше как при остановке репортёром); связь
  # снова не вернулась — строка остаётся и становится FAIL version-guard-error ниже
  # (остановка уже есть — такие границы покрыты ею и строк не дают)
  if [ -s "$PW_DIR/version-guard-errors" ]; then
    _gtmp="$PW_DIR/version-guard-errors.tmp"
    : >"$_gtmp"
    while IFS="$TAB" read -r _gb _gc || [ -n "$_gb" ]; do
      [ -n "$_gb" ] || continue
      if [ "$_gc" = 3 ]; then
        [ ! -f "$PW_DIR/version-stopped" ] || continue
        _grc=0
        rt_version_guard "$ID" "$_gb" || _grc=$?
        case "$_grc" in
          0) continue ;;
          4)
            printf '%s\n' "$_gb" >"$PW_DIR/version-stopped"
            continue
            ;;
        esac
        _gc=$_grc
      fi
      printf '%s\t%s\n' "$_gb" "$_gc" >>"$_gtmp"
    done <"$PW_DIR/version-guard-errors"
    mv "$_gtmp" "$PW_DIR/version-guard-errors"
  fi
  _stopped=0
  [ ! -f "$PW_DIR/version-stopped" ] || _stopped=1
  rt_redact <"$LOG" >"$LOG.red" && mv "$LOG.red" "$LOG"
  : >"$PW_DIR/parsed.tsv"
  : >"$PW_DIR/files.tsv"
  : >"$PW_DIR/interrupted.tsv"
  if [ -f "$_fj" ]; then
    if [ "$_stopped" = 1 ]; then
      node "$SCRIPT_DIR/pw-parse.js" "$_fj" --files-out "$PW_DIR/files.tsv" \
        --interrupted-out "$PW_DIR/interrupted.tsv" >>"$PW_DIR/parsed.tsv" || true
    else
      node "$SCRIPT_DIR/pw-parse.js" "$_fj" --files-out "$PW_DIR/files.tsv" >>"$PW_DIR/parsed.tsv" || true
    fi
  fi
  _found=0
  RUN_BAD=0
  QFAILS=0
  while IFS="$TAB" read -r st chk det || [ -n "$st" ]; do
    [ -n "$st" ] || continue
    _found=$((_found + 1))
    rt_result "$ID" "$st" "pw:$chk" "$det"
    case "$st" in
      FAIL | XPASS) RUN_BAD=$((RUN_BAD + 1)) ;;
      QFAIL) QFAILS=$((QFAILS + 1)) ;;
    esac
  done <"$PW_DIR/parsed.tsv"

  if [ "$_found" = 0 ] && [ "$_frc" = 0 ] && [ -z "$SWITCH" ] && { [ "${RT_PW_CORE_ONLY:-}" = 1 ] || [ -n "${RT_PW_GREP_INVERT:-}" ]; }; then
    # отбор @core или отсев --grep-invert не оставил тестов: строк результата нет и FAIL pw:run тоже
    :
  elif [ "$_found" = 0 ] && [ "$_stopped" = 1 ] && [ -s "$PW_DIR/interrupted.tsv" ]; then
    # остановка по смене версии прервала все тесты запуска: они — строки notrun ниже
    :
  elif [ "$_found" = 0 ]; then
    # отчёта нет или он пуст: причина — в журнале запуска (панель не отвечает и т. п.)
    reason=$(grep -m 1 -E 'панель не отвечает|вход в панель не удался|не заданы переменные' "$LOG" || true)
    [ -n "$reason" ] || reason="Playwright завершился с кодом $_frc без результатов: $(tail -n 3 "$LOG" | tr '\n' ' ')"
    rt_result "$ID" FAIL "pw:run" "$reason"
    RUN_BAD=$((RUN_BAD + 1))
  elif [ "$_frc" != 0 ] && [ "$RUN_BAD" = 0 ] && [ "$QFAILS" = 0 ] && [ "$_stopped" = 0 ]; then
    rt_result "$ID" FAIL "pw:run" "Playwright завершился с кодом $_frc без упавших тестов"
    RUN_BAD=$((RUN_BAD + 1))
  fi
  # сама сверка версии не выполнилась (код version-guard.sh не 0, 3 и 4): открытый отказ запрещён
  if [ -s "$PW_DIR/version-guard-errors" ]; then
    while IFS="$TAB" read -r _gb _gc || [ -n "$_gb" ]; do
      [ -n "$_gb" ] || continue
      rt_result "$ID" FAIL version-guard-error "сверка версии стенда не выполнена (код ${_gc:--}, граница $_gb)"
      RUN_BAD=$((RUN_BAD + 1))
    done <"$PW_DIR/version-guard-errors"
  fi
  BAD=$((BAD + RUN_BAD))

  [ "$_stopped" = 1 ] || return 0
  # падения спеков, не прошедших границу до остановки: строки parsed.tsv и files.tsv идут в одном
  # порядке (global-setup — в конце parsed.tsv, без спека)
  awk -F "$TAB" -v core="$CORE_DIR" '
    FILENAME == ARGV[1] { ok[$0] = 1; next }
    FILENAME == ARGV[2] { spec[FNR] = $2; next }
    ($1 == "FAIL" || $1 == "XPASS") && !((FNR in spec) && (spec[FNR] in ok)) {
      printf "suspect\t%s\tpw:%s\n", core, $2
    }
  ' "$PW_DIR/version-ok-specs.txt" "$PW_DIR/files.tsv" "$PW_DIR/parsed.tsv" >>"$VSTOP"
  # прерванные и не начатые тесты — по строке notrun на спек, в порядке появления
  awk -F "$TAB" -v core="$CORE_DIR" '
    !($1 in n) { order[++k] = $1 }
    { n[$1]++ }
    END {
      for (i = 1; i <= k; i++)
        printf "notrun\t%s\tPlaywright: %s (прервано остановкой, тестов %d)\n", core, order[i], n[order[i]]
    }
  ' "$PW_DIR/interrupted.tsv" >>"$VSTOP"
}

# --- артефакты падения (D-16): хвост xcp.log ---------------------------------------
pull_tail() {
  TAIL="$RT_REPORT/$ID/$CORE_DIR$SUB/xcp-tail.log"
  rt_ssh "$ID" "tail -n 200 /opt/var/log/xcp.log" 2>/dev/null | rt_redact >"$TAIL" || true
  [ -s "$TAIL" ] || echo "хвост xcp.log недоступен" >"$TAIL"
}

# stop_check <спеки следующих запусков...>: репортёр сверки версии (version-reporter.ts) записал
# признак остановки — версия стенда сменилась после спека этого запуска (D-13). Следующие запуски
# не начинаются: их спеки — строки notrun в version-stop.tsv; хвост xcp.log, как при падении;
# выход 4. Строки stop и FAIL version-guard уже записал version-guard.sh.
stop_check() {
  [ -f "$PW_DIR/version-stopped" ] || return 0
  for _s in "$@"; do
    printf 'notrun\t%s\tPlaywright: tests/router/%s\n' "$CORE_DIR" "${_s#tests/router/}" >>"$VSTOP"
  done
  pull_tail
  exit 4
}

# Запуски по порядку: часть read (pw-read, report-pages.json), затем часть state — первый
# запуск в pw, второй (RT_SPECS_FIRST) в pw-2; --switch — pw-switch. Каждый запуск со своим
# каталогом, state.json и JSON-отчётом. После каждого запуска — проверка остановки по смене
# версии (stop_check): следующие запуски при ней не идут.
VSTOP="$RT_REPORT/$ID/version-stop.tsv"
RUNS=0
if [ -n "$READ_SPECS" ]; then
  use_dir pw-read
  : >"$LOG"
  rc=0
  # shellcheck disable=SC2086
  run_pw "$PW_DIR/report-pages.json" "$PAGE_WORKERS" $READ_SPECS || rc=$?
  finish_run "$PW_DIR/report-pages.json" "$rc"
  RUNS=$((RUNS + 1))
  # shellcheck disable=SC2086
  stop_check $FIRST_SPECS $SECOND_SPECS
fi
if [ -n "$FIRST_SPECS" ]; then
  if [ -n "$SWITCH" ]; then use_dir pw-switch; else use_dir pw; fi
  : >"$LOG"
  rc=0
  # shellcheck disable=SC2086
  run_pw "$PW_DIR/report.json" 1 $FIRST_SPECS || rc=$?
  finish_run "$PW_DIR/report.json" "$rc"
  RUNS=$((RUNS + 1))
  # shellcheck disable=SC2086
  stop_check $SECOND_SPECS
fi
if [ -n "$SECOND_SPECS" ]; then
  use_dir pw-2
  : >"$LOG"
  rc=0
  # shellcheck disable=SC2086
  run_pw "$PW_DIR/report.json" 1 $SECOND_SPECS || rc=$?
  finish_run "$PW_DIR/report.json" "$rc"
  RUNS=$((RUNS + 1))
  stop_check
fi
if [ "$RUNS" = 0 ]; then
  # без --part и без спеков: запуска не было — FAIL pw:run, журнал в pw/
  use_dir pw
  : >"$LOG"
  finish_run "$PW_DIR/report.json" 0
fi

[ "$BAD" = 0 ] || pull_tail

[ "$BAD" = 0 ] || exit 1
exit 0
