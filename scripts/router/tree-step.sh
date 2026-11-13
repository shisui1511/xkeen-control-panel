#!/bin/sh
# scripts/router/tree-step.sh — шаг поиска виновного коммита: один тест на дереве коммита (D-22, todo 11).
#
#   tree-step.sh <ref> <цель> <ядро> (--go <каталог пакета>:<TestRouterИмя> | --spec tests/router/<спек>.spec.ts
#                [--grep <регэксп>]) --out <каталог шага под build/router>
#                                  деплой сборки коммита, при нужде переключение ядра, один тест
#   tree-step.sh prepare <ref>     только подготовить (или переиспользовать) worktree дерева коммита
#                                  и напечатать его путь (проход tree сессии ставит сборку дерева
#                                  один раз на проход)
#   tree-step.sh cleanup           разблокировать и снять все worktree build/bisect/wt-*
#
# Продукт и тест берутся из дерева коммита целиком: отдельный заблокированный worktree
# <основная копия>/build/bisect/wt-<короткий sha> (git worktree add --detach --lock), рабочее дерево
# прогона не трогается. Обвязка — скрипты этого каталога (HEAD): deploy.sh, pw-suite.sh --switch,
# go-suite.sh, pw-suite.sh запускаются с текущим каталогом worktree и RT_LOCK_HELD=1. В worktree
# build/router — ссылка на build/router вызывающей копии (основной или worktree агента: отчёт шага
# и его артефакты физически лежат в отчёте, который читает bisect.mjs),
# frontend/node_modules — ссылка на node_modules основной копии при совпадающем package-lock.json,
# иначе npm ci. Worktree переиспользуется, если уже стоит на том же sha.
#
# Шаг 4а: при RT_RTENV=1 (run.sh ставит его набору bisect) после деплоя и ожидания панели блок
# сокращённых сроков xcp-rt пишется заново (rtenv.sh write, журнал rtenv-step.log): код 3 — обрыв
# связи, иной сбой — ошибка шага (код 1, не по сборке). Без RT_RTENV или не 1 — шаг пропускается.
#
# Вывод шага: строки RESULT=PASS|FAIL|SKIP и REASON=<кратко>.
#   PASS  тест прошёл (PASS, KNOWN, QPASS), FAIL — упал (FAIL, XPASS, QFAIL хоть одной строкой);
#   SKIP  теста нет в дереве, сборка или установка сборки не удалась, тест не выполнился (D-22:
#         шаг, который нельзя проверить, поиск пропускает и берёт соседа).
# Артефакты шага — в <out>: deploy-step.log, rtenv-step.log (шаг 4а), test-step.log, <цель>/test.tsv (строки теста),
# <цель>/switch.tsv (переключение ядра), result.txt.
#
# Коды выхода: 0 — шаг выполнен (в том числе SKIP), 1 — ошибка шага (worktree, переключение ядра,
# установка не по сборке), 2 — неверные аргументы, 3 — обрыв связи.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

ROOT=$(rt_main_root)
BISECT_BASE="$ROOT"/build/bisect
NL='
'

ts_die() {
  echo "tree-step: $*" >&2
  exit 2
}

# Значение целиком соответствует расширенному регулярному выражению (без переводов строки).
ts_match() {
  case "$2" in
    *"$NL"*) return 1 ;;
  esac
  printf '%s\n' "$2" | grep -Eq "$1"
}

REF_RE='^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$'
GO_RE='^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$'
SPEC_RE='^tests/router/[A-Za-z0-9_.-]+\.spec\.ts$'

# Ссылка -> SHA (40 hex) и короткий sha; неверная ссылка — код 2.
ts_resolve() {
  ts_match "$REF_RE" "$1" || ts_die "ref: недопустимая ссылка"
  SHA=$(git rev-parse --verify --quiet "$1^{commit}") || ts_die "ref: '$1' не указывает на коммит"
  SHORT=$(git rev-parse --short=8 "$SHA")
  WT="$BISECT_BASE/wt-$SHORT"
}

# Снятие worktree: сначала ссылки (в основную копию не уходит ничего), затем unlock и remove.
ts_wt_remove() {
  [ ! -L "$1/build"/router ] || rm -f "$1/build"/router
  [ ! -L "$1/frontend/node_modules" ] || rm -f "$1/frontend/node_modules"
  git -C "$ROOT" worktree unlock "$1" >/dev/null 2>&1 || true
  git -C "$ROOT" worktree remove --force "$1" >/dev/null 2>&1 || true
  git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
}

# Worktree дерева коммита: SHA и WT уже заданы ts_resolve. Код 0 — готов; 1 — не создан;
# 4 — не установились зависимости фронтенда (шаг SKIP «сборка не удалась»).
ts_wt_prepare() {
  _reuse=0
  if [ -e "$WT/.git" ]; then
    _cur=$(git -C "$WT" rev-parse HEAD 2>/dev/null || true)
    if [ "$_cur" = "$SHA" ]; then
      _reuse=1
    else
      echo "tree-step: worktree на другом коммите — снимается: $WT" >&2
      ts_wt_remove "$WT"
    fi
  fi
  if [ "$_reuse" = 0 ]; then
    [ ! -e "$WT" ] || {
      echo "tree-step: путь worktree занят не worktree: $WT" >&2
      return 1
    }
    mkdir -p "$BISECT_BASE"
    git -C "$ROOT" worktree add --detach --lock --reason router-test-bisect "$WT" "$SHA" >&2 || return 1
  fi
  mkdir -p "$WT/build"
  # ссылка — на отчёты вызывающей копии, как у session.mjs enterTree: из worktree агента test.tsv и
  # switch.tsv шага иначе уходили бы в build/router основной копии, мимо отчёта поиска
  ln -sfn "$(rt_repo_root)"/build/router "$WT/build"/router
  if [ -f "$WT/frontend/package.json" ] && [ ! -e "$WT/frontend/node_modules" ]; then
    if [ -d "$ROOT/frontend/node_modules" ] && cmp -s "$ROOT/frontend/package-lock.json" "$WT/frontend/package-lock.json"; then
      ln -s "$ROOT/frontend/node_modules" "$WT/frontend/node_modules"
    else
      echo "tree-step: package-lock.json отличается от основной копии: npm ci" >&2
      npm --prefix "$WT/frontend" ci >&2 || return 4
    fi
  fi
  return 0
}

# --- подкоманды prepare и cleanup ------------------------------------------------------

case "${1:-}" in
  prepare)
    [ $# -eq 2 ] || ts_die "usage: tree-step.sh prepare <ref>"
    ts_resolve "$2"
    _prc=0
    ts_wt_prepare || _prc=$?
    if [ "$_prc" != 0 ]; then
      echo "tree-step: worktree дерева коммита не подготовлен (код $_prc)" >&2
      exit 1
    fi
    printf '%s\n' "$WT"
    exit 0
    ;;
  cleanup)
    [ $# -eq 1 ] || ts_die "usage: tree-step.sh cleanup"
    for _w in "$BISECT_BASE"/wt-*; do
      [ -e "$_w" ] || continue
      ts_wt_remove "$_w"
    done
    git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
    rmdir "$BISECT_BASE" >/dev/null 2>&1 || true
    exit 0
    ;;
esac

# --- разбор аргументов шага -------------------------------------------------------------

[ $# -ge 3 ] || ts_die "usage: tree-step.sh <ref> <цель> <ядро> (--go <пакет>:<Тест> | --spec <спек> [--grep <re>]) --out <каталог>"
REF=$1
ID=$2
CORE=$3
shift 3
GO=""
SPEC=""
GREP=""
OUT=""
while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || ts_die "$1 требует значения"
  case "$1" in
    --go) GO=$2 ;;
    --spec) SPEC=$2 ;;
    --grep) GREP=$2 ;;
    --out) OUT=$2 ;;
    *) ts_die "неизвестный аргумент: $1" ;;
  esac
  shift 2
done

ts_resolve "$REF"
case "$ID" in '' | *[!a-z0-9]*) ts_die "цель: допустимы a-z и 0-9" ;; esac
case "$CORE" in xray | mihomo) ;; *) ts_die "ядро: xray или mihomo" ;; esac
if [ -n "$GO" ] && [ -n "$SPEC" ]; then ts_die "нужен один из --go и --spec"; fi
if [ -z "$GO" ] && [ -z "$SPEC" ]; then ts_die "нужен --go или --spec"; fi
if [ -n "$GO" ]; then
  ts_match "$GO_RE" "$GO" || ts_die "--go: ожидается <каталог пакета>:TestRouter<Имя>"
  PKG=${GO%%:*}
  PKG=${PKG#./}
  TNAME=${GO#*:}
  [ -z "$GREP" ] || ts_die "--grep относится только к --spec"
else
  ts_match "$SPEC_RE" "$SPEC" || ts_die "--spec: ожидается tests/router/<имя>.spec.ts"
  case "$GREP" in
    *"$NL"* | *"'"*) ts_die "--grep: недопустимы одинарная кавычка и перевод строки" ;;
  esac
fi
[ -n "$OUT" ] || ts_die "нужен --out"

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
rt_lock || exit 1

# Каталог шага: проверка пути — от корня вызывающей копии (rt_report_use), сам отчёт пишется
# через ссылку build/router worktree, физически в ту же вызывающую копию.
RT_REPORT=$OUT
rt_report_use
_root=$(rt_repo_root)
REL=${RT_REPORT#"$_root"/build/router/}

# Окружение набора и прохода не должно просочиться в этапы шага.
unset RT_RESULTS_NAME RT_ART_SUB RT_SPECS RT_SPECS_FIRST RT_PW_GREP RT_PW_GREP_INVERT RT_PW_CORE_ONLY \
  RT_GO_PKGS RT_GO_RUN RT_GO_RUN_SEQ RT_CORE RT_REPORT_DIR XCP_WANT_CORE
RT_LOCK_HELD=1
export RT_LOCK_HELD

trap 'exit 130' INT TERM HUP

# Исход шага: ts_finish <PASS|FAIL|SKIP> <причина>.
ts_finish() {
  printf 'RESULT=%s\nREASON=%s\n' "$1" "$2"
  printf 'RESULT=%s\nREASON=%s\n' "$1" "$2" >"$RT_REPORT/result.txt" 2>/dev/null || true
  exit 0
}

# 1. Есть ли тест в дереве коммита.
if [ -n "$GO" ]; then
  if ! git grep -qF -e "func $TNAME(" "$SHA" -- ":(glob)$PKG/*_test.go" >/dev/null 2>&1; then
    ts_finish SKIP "теста нет в дереве"
  fi
else
  if ! git cat-file -e "$SHA:frontend/$SPEC" >/dev/null 2>&1; then
    ts_finish SKIP "теста нет в дереве"
  fi
fi

# 2. Дерево коммита: worktree со ссылками.
_wrc=0
ts_wt_prepare || _wrc=$?
case "$_wrc" in
  0) ;;
  4) ts_finish SKIP "сборка не удалась (зависимости фронтенда)" ;;
  *)
    echo "tree-step: worktree дерева коммита не создан" >&2
    exit 1
    ;;
esac

# Отчёт шага для этапов — внутри worktree (ссылка build/router на вызывающую копию).
STEP_REPORT="$WT/build"/router/$REL
mkdir -p "$STEP_REPORT"
mkdir -p "$STEP_REPORT/$ID"

# 3. Деплой сборки дерева коммита (deploy.sh HEAD, текущий каталог — worktree).
_drc=0
(
  cd "$WT"
  RT_REPORT=$STEP_REPORT sh "$SCRIPT_DIR/deploy.sh" --target "$ID"
) >"$STEP_REPORT/deploy-step.log" 2>&1 || _drc=$?
if [ -f "$STEP_REPORT/$ID/deploy.rc" ]; then
  case "$(cat "$STEP_REPORT/$ID/deploy.rc")" in
    0) ;;
    3) exit 3 ;;
    1) ts_finish SKIP "установка сборки не удалась (xcp не запустился или версия не совпала)" ;;
    *)
      echo "tree-step: деплой не по сборке (подготовка стенда), см. $STEP_REPORT/deploy-step.log" >&2
      exit 1
      ;;
  esac
elif grep -q '^deploy: сборка ветки' "$STEP_REPORT/deploy-step.log"; then
  ts_finish SKIP "сборка не удалась (код $_drc)"
else
  echo "tree-step: деплой не дошёл до сборки (код $_drc), см. $STEP_REPORT/deploy-step.log" >&2
  exit 1
fi

# 4. Ядро: переключение, если активное на цели не то (как session.mjs switchTo).
# deploy.sh сверяет только xcp -v бинарника: новая панель ещё может подниматься (прежняя только
# что остановлена), а переключение идёт через её API — сначала дождаться ответа /api/version.
if ! RT_REBOOT_WAIT=${RT_TREE_PANEL_WAIT:-120} rt_wait_back "$ID"; then
  if ! rt_ssh "$ID" true >/dev/null 2>&1; then
    exit 3
  fi
  echo "tree-step: панель сборки $SHORT не ответила на /api/version за $RT_BACK_SECS с после деплоя" >&2
  exit 1
fi
_src=0
_now=$(rt_ssh "$ID" 'x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); if [ -n "$x" ] && [ -n "$m" ]; then echo both; elif [ -n "$x" ]; then echo xray; elif [ -n "$m" ]; then echo mihomo; else echo none; fi' 2>/dev/null) || _src=$?
[ "$_src" != 3 ] || exit 3
if [ "$_now" != "$CORE" ]; then
  _swrc=0
  (
    cd "$WT"
    RT_REPORT=$STEP_REPORT RT_RESULTS_NAME=switch.tsv RT_ART_SUB=tree-switch \
      sh "$SCRIPT_DIR/pw-suite.sh" "$ID" "$CORE" --switch "$CORE"
  ) >"$STEP_REPORT/switch-step.log" 2>&1 || _swrc=$?
  if [ "$_swrc" != 0 ]; then
    if ! rt_ssh "$ID" true >/dev/null 2>&1; then
      exit 3
    fi
    echo "tree-step: переключение ядра на $CORE не удалось (код $_swrc), см. $STEP_REPORT/switch-step.log" >&2
    exit 1
  fi
fi

# 4а. Сокращённые сроки (D-22): деплой шага снял блок xcp-rt (D-23) — при RT_RTENV=1 (run.sh ставит
# его набору bisect) блок пишется заново, чтобы коммит судился на тех же сроках, что и гейт.
# Снимать не нужно: bisect.mjs возвращает стенд по снимку, снятому до первого шага (без блока).
if [ "${RT_RTENV:-0}" = 1 ]; then
  _rerc=0
  sh "$SCRIPT_DIR/rtenv.sh" "$ID" write >"$STEP_REPORT/rtenv-step.log" 2>&1 || _rerc=$?
  [ "$_rerc" != 3 ] || exit 3
  if [ "$_rerc" != 0 ]; then
    echo "tree-step: блок сокращённых сроков не записан (код $_rerc), см. $STEP_REPORT/rtenv-step.log" >&2
    exit 1
  fi
fi

# 5. Один тест (этапы HEAD в каталоге дерева коммита).
_trc=0
if [ -n "$GO" ]; then
  (
    cd "$WT"
    RT_REPORT=$STEP_REPORT RT_RESULTS_NAME=test.tsv RT_GO_RUN_SEQ="$PKG:$TNAME" \
      sh "$SCRIPT_DIR/go-suite.sh" "$ID" "$CORE"
  ) >"$STEP_REPORT/test-step.log" 2>&1 || _trc=$?
else
  (
    cd "$WT"
    RT_REPORT=$STEP_REPORT RT_RESULTS_NAME=test.tsv RT_SPECS="$SPEC" RT_PW_GREP="$GREP" \
      sh "$SCRIPT_DIR/pw-suite.sh" "$ID" "$CORE"
  ) >"$STEP_REPORT/test-step.log" 2>&1 || _trc=$?
fi
[ "$_trc" != 3 ] || exit 3
# Код 4 — смена версии стенда во время шага: тест мог упасть на чужой версии (класс stand), вердикт
# по нему вынес бы виновным не тот коммит. Шаг не проверен — SKIP.
[ "$_trc" != 4 ] || ts_finish SKIP "смена версии стенда во время шага (код 4): шаг не проверен"

# 6. Исход по строкам теста: go — <Имя> и <Имя>/…; pw — любые тестовые строки (кроме pw:run, pw:setup).
TESTS_TSV="$STEP_REPORT/$ID/test.tsv"
if [ ! -f "$TESTS_TSV" ]; then
  if ! rt_ssh "$ID" true >/dev/null 2>&1; then
    exit 3
  fi
  ts_finish SKIP "тест не выполнился (нет результатов, код $_trc)"
fi
if [ -n "$GO" ]; then
  _verdict=$(awk -F'\t' -v n="go:$TNAME" '
    $3 == n || index($3, n "/") == 1 {
      if ($1 == "FAIL" || $1 == "XPASS" || $1 == "QFAIL") bad++
      else if ($1 == "PASS" || $1 == "KNOWN" || $1 == "QPASS") ok++
    }
    END { if (bad > 0) print "FAIL"; else if (ok > 0) print "PASS"; else print "NONE" }
  ' "$TESTS_TSV")
else
  _verdict=$(awk -F'\t' '
    $3 ~ /^pw:/ && $3 != "pw:run" && $3 != "pw:setup" {
      if ($1 == "FAIL" || $1 == "XPASS" || $1 == "QFAIL") bad++
      else if ($1 == "PASS" || $1 == "KNOWN" || $1 == "QPASS") ok++
    }
    END { if (bad > 0) print "FAIL"; else if (ok > 0) print "PASS"; else print "NONE" }
  ' "$TESTS_TSV")
fi
case "$_verdict" in
  PASS) ts_finish PASS "тест прошёл на сборке дерева коммита" ;;
  FAIL) ts_finish FAIL "тест упал на сборке дерева коммита" ;;
esac
if ! rt_ssh "$ID" true >/dev/null 2>&1; then
  exit 3
fi
ts_finish SKIP "тест не выполнился (код этапа $_trc)"
