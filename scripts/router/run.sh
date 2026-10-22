#!/bin/sh
# scripts/router/run.sh [SUITE] — проверка на роутерах (make router-test SUITE=...).
#
# Наборы:
#   (без SUITE)  полный: деплой, снимок, затем матрица ядер (D-20) — для каждого ядра
#                из RT_CORES ветвь «сброс записи на носитель (sync) -> переключение ядра
#                через интерфейс панели (pw-suite.sh --switch) -> смоук без окна
#                (smoke.sh --no-observe) -> наблюдатель окна (smoke.sh --observe-only)
#                параллельно с частью Playwright только чтения (pw-suite.sh --part read) ->
#                Go-тесты на устройстве (go-suite.sh) -> ожидание стенда (settle) -> часть
#                Playwright, меняющая состояние (pw-suite.sh --part state)»; затем
#                восстановление, сверка и проверка core-restored. Параллельно окну идут только
#                спеки только чтения: Go-тесты и изменяющие спеки стартуют после ожидания
#                наблюдателя (D-07). Полный прогон — полная матрица: все тесты под обоими
#                ядрами; неразмеченный тест с разным исходом в ветвях даёт строку
#                FAIL core-mark:<проверка> «нужна метка ядра» и раздел «Нужна метка ядра» в
#                summary.md (D-05б).
#   smoke        деплой и смоук (только чтение, без снимка).
#   changed      быстрый набор для каждой задачи (D-25): select-changed.mjs по diff от
#                origin/main и незакоммиченным правкам выбирает Go-пакеты и роутерные спеки.
#                Ветви строятся гибридом (D-01…D-03): первая — ядро из SHA HEAD (cksum SHA
#                чётный — xray, нечётный — mihomo; повтор на том же HEAD идёт под тем же
#                ядром) со всем выбором; вторая — другое ядро и только тесты с меткой ядра
#                (Go: RT_GO_CORE_PKGS по RT_GO_CORE_RUN, Playwright: RT_SPECS_CORE с отбором
#                @core). Нет размеченных тестов — второй ветви нет вовсе (ни sync, ни
#                переключения, ни смоука, ни окна). Окно смоука — 30 с, а при изменённом коде
#                запуска ядра, watchdog или полном выборе (RT_KERNEL_TOUCHED=1) — 120 с (D-07).
#                Ничего не затронуто — деплой и смоук (без снимка). Причины выбора —
#                <отчёт>/select.txt, план ветвей и окно — <отчёт>/plan.txt, оба в разделе
#                «Выбор» отчёта; раскладка тестов по ветвям — <отчёт>/<цель>/layout.tsv.
#   session      сессия на стенде по спеке (SPEC=<файл JSON>): проходы с ожиданиями на одной
#                цели и ядрах спеки, в том числе на файлах продукта из другого коммита
#                (воспроизведение до исправления). Проходы ведёт session.mjs (формат спеки —
#                в его заголовке), этапы — те же deploy.sh, smoke.sh, pw-suite.sh и
#                go-suite.sh. В конце рабочее дерево и стенд возвращаются: сборка HEAD,
#                исходное ядро. Итог — summary.md и session.json отчёта. Нужно чистое
#                рабочее дерево; снимка нет, как у ручной сессии на стенде.
#   reboot       холодный старт (D-24): деплой, предполёт и снимок параллельно, затем
#                по одной цели в порядке XCP_REBOOT_ORDER (нижестоящая цель каскада первой):
#                перезагрузка, ожидание возврата, проверки автозапуска (xcp той же версии,
#                XKeen, то же ядро, правила iptables), смоук, сверка со снимком. В конце
#                все цели снова отвечают (reboot-reachable-end). Матрица ядер не применяется.
#
# Порядок: check-known.sh (метки известных падений) -> цели из локального конфига ->
# блокировка на весь прогон -> отчёт -> проверка связи -> deploy.sh -> по целям
# параллельно [preflight -> снимок -> ветви ядер (переключение, смоук, Go-тесты, Playwright)
# -> восстановление + сверка] -> report.md и summary.md в build/router/<время>-<sha>/ (ссылка build/router/last).
# Восстановление стоит в trap подпроцесса цели сразу после снимка: оно выполняется
# при любом исходе, в том числе при падении этапов посередине. Сигнал, полученный верхним
# run.sh, пересылается подпроцессам целей (под dash они сами SIGINT не перехватывают);
# повторный сигнал восстановление не прерывает: _term и восстановление его игнорируют.
#
# Окружение:
#   ROUTERS=<id,id>      выбор целей (по умолчанию все из конфига)
#   RELEASE=<тег>        ставить артефакт GitHub Release вместо сборки ветки
#   SPEC=<файл>          спека набора session (путь от корня рабочей копии или абсолютный)
#   RT_HOLD_MINUTES=<N>  после прогона держать блокировку N минут для ручной
#                        проверки; снять раньше: touch <git-common-dir>/router-test.release
#   RT_LOCK_WAIT=<с>     ожидание очереди на роутеры (по умолчанию 3600)
#   RT_SMOKE_WINDOW=<с>  окно наблюдения смоука за перезапуском ядра (по умолчанию 120; в наборе
#                        changed без этой переменной — 30 с, при RT_KERNEL_TOUCHED=1 — 120 с)
#   RT_SMOKE_IPT_WAIT=<с> предел ожидания правил iptables в смоуке (по умолчанию 120); не
#                        зависит от длины окна
#   RT_CORES="<ядра>"    ветви матрицы через пробел (по умолчанию «xray mihomo»); явный RT_CORES —
#                        ручная отладка: гибрид changed выключается, все выбранные тесты идут в
#                        каждой ветви. Для гейта и обычной работы не задавать
#   RT_REBOOT_WAIT=<с>   ожидание возврата цели после перезагрузки (по умолчанию 900)
#   XCP_REBOOT_ORDER     порядок перезагрузки (локальный конфиг, id через пробел)
#   RT_BASE=<ref>        база сравнения набора changed (по умолчанию merge-base с origin/main)
#   RT_FILES=<a,b,c>     набор changed: выбор для заданного списка файлов вместо git (отладка)
#
# Раскладка отчёта: результаты всех ветвей — в <отчёт>/<id>/results.tsv (ядро ветви — во
# второй колонке), артефакты ветви (go-*.jsonl, pw-read/, pw/, pw-switch/) — в
# <отчёт>/<id>/<ядро>/: pw-read/ — часть Playwright только чтения, pw/ — изменяющая часть.
# Этапы ветви в timings.tsv: sync-<ядро>, switch-<ядро>, smoke-<ядро> (проверки без окна),
# observe-<ядро> (окно, идёт параллельно pw-read-<ядро>), pw-read-<ядро>, go-<ядро>,
# settle-<ядро>, pw-<ядро> (часть state). Хвост xcp.log ветви пишет наблюдатель окна.
# Этап, оборванный сигналом, — строка с кодом 130 (её пишет _term), начало и ядро ветви те же.
#
# Код выхода: 0 — нет строк FAIL и XPASS, 2 — неизвестный набор, иначе 1. Набор session —
# код session.mjs: 0 — ожидания совпали и стенд вернулся, 3 — сбой связи ПК↔стенд, иначе 1 или 2.
# 130 — прерван сигналом INT, TERM или HUP: этап остановлен, стенд восстановлен по снимку,
# отчёт с пометкой «Прерван» написан, удержание блокировки (RT_HOLD_MINUTES) не применяется.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

SUITE=${1:-}
case "$SUITE" in
  smoke | changed | reboot | session | '') ;;
  *)
    echo "router-test: неизвестный набор '${SUITE}'. Поддерживаются: smoke, changed, reboot, session и полный (без SUITE)" >&2
    echo "router-test: пример: make router-test SUITE=smoke" >&2
    exit 2
    ;;
esac
SUITE_NAME=${SUITE:-full}

# Ветви матрицы ядер: только xray и mihomo, без повторов. Явно заданный RT_CORES (даже
# равный умолчанию) — ручная отладка: гибрид набора changed выключается.
RT_CORES_SET=${RT_CORES+1}
RT_CORES=${RT_CORES:-xray mihomo}
_seen=""
for _c in $RT_CORES; do
  case "$_c" in
    xray | mihomo) ;;
    *)
      echo "router-test: RT_CORES: неизвестное ядро '$_c' (допустимы xray и mihomo)" >&2
      exit 2
      ;;
  esac
  case " $_seen " in
    *" $_c "*)
      echo "router-test: RT_CORES: ядро '$_c' указано дважды" >&2
      exit 2
      ;;
  esac
  _seen="$_seen $_c"
done

# Ядро первой ветви набора changed (D-02): чётная контрольная сумма cksum строки SHA HEAD
# (без перевода строки) — xray, нечётная — mihomo. Зависит только от SHA: повтор на том же
# HEAD идёт под тем же ядром, а на разных коммитах ядра чередуются.
rt_sha_core() {
  _shc=$(printf '%s' "$1" | cksum | cut -d' ' -f1)
  if [ $((_shc % 2)) = 0 ]; then
    echo xray
  else
    echo mihomo
  fi
}

cd "$(rt_repo_root)"

# Набор session: спека и чистое дерево проверяются до блокировки, цель берётся из спеки.
if [ "$SUITE" = session ]; then
  if [ -z "${SPEC:-}" ]; then
    echo "router-test: SUITE=session требует SPEC=<файл спеки>" >&2
    exit 2
  fi
  ROUTERS=$(node "$SCRIPT_DIR/session.mjs" --target "$SPEC") || exit 2
  export ROUTERS
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    echo "router-test: SUITE=session требует чистое рабочее дерево: проходы меняют файлы продукта" >&2
    exit 2
  fi
fi

# Метки известных падений должны ссылаться на живые todo (D-13): до блокировки и деплоя.
if ! sh "$SCRIPT_DIR/check-known.sh"; then
  echo "router-test: красный — метки известных падений ссылаются на закрытые или несуществующие todo" >&2
  exit 1
fi

rt_load_targets
rt_lock
rt_report_init
echo "router-test: отчёт $RT_REPORT"

# Удержание блокировки после прогона для ручной проверки (RT_HOLD_MINUTES).
hold_lock() {
  if [ -n "${RT_HOLD_MINUTES:-}" ] && [ "$RT_HOLD_MINUTES" -gt 0 ] 2>/dev/null; then
    RELEASE_FILE="$(rt_common_dir)/router-test.release"
    rm -f "$RELEASE_FILE"
    # INT/TERM/HUP прерывают удержание (код 130); EXIT убирает файл-флаг при любом выходе
    trap 'rm -f "$RELEASE_FILE"' EXIT
    trap 'exit 130' INT TERM HUP
    echo "router-test: блокировка удерживается $RT_HOLD_MINUTES мин; снять раньше: touch $RELEASE_FILE"
    END=$(($(date +%s) + RT_HOLD_MINUTES * 60))
    while [ "$(date +%s)" -lt "$END" ] && [ ! -e "$RELEASE_FILE" ]; do
      sleep 1
    done
    rm -f "$RELEASE_FILE"
  fi
}

# --- набор session ------------------------------------------------------------------
# Сессию ведёт session.mjs под блокировкой этого процесса. Сигнал передаётся ему, и run.sh
# ждёт, пока он вернёт стенд: по признаку жизни (kill -0), а не по коду wait, как у матрицы ниже.
if [ "$SUITE" = session ]; then
  node "$SCRIPT_DIR/session.mjs" "$SPEC" &
  SPID=$!
  trap 'kill -TERM "$SPID" 2>/dev/null || true' INT TERM HUP
  SRC=0
  wait "$SPID" || SRC=$?
  while kill -0 "$SPID" 2>/dev/null; do
    SRC=0
    wait "$SPID" || SRC=$?
  done
  trap - INT TERM HUP
  # Прерванная сессия (код 130) блокировку не удерживает, как и прерванная матрица
  [ "$SRC" = 130 ] || hold_lock
  if [ "$SRC" = 0 ]; then
    echo "router-test: зелёный"
  else
    echo "router-test: сессия завершилась с кодом $SRC (итог — $RT_REPORT/summary.md)" >&2
  fi
  exit "$SRC"
fi

# --- выбор проверок набора changed (D-25) ------------------------------------------
# select-changed.mjs печатает восемь строк для eval; перед eval вывод проверяется по
# строгому шаблону на каждую строку и по их числу (T-144.1-23, T-145.1-18), при любом
# сомнении выбирается всё, а вторая ветвь идёт со всеми тестами (RT_SEL_BAD=1).
SEL_ANY=1
RT_SEL_BAD=0
RT_KERNEL_TOUCHED=0
RT_GO_CORE_PKGS=""
RT_GO_CORE_RUN=""
RT_SPECS_CORE=""
if [ "$SUITE" = changed ]; then
  _sel_args=""
  [ -z "${RT_BASE:-}" ] || _sel_args="--base $RT_BASE"
  [ -z "${RT_FILES:-}" ] || _sel_args="--files $RT_FILES"
  _sel_rc=0
  # shellcheck disable=SC2086
  _sel=$(node "$SCRIPT_DIR/select-changed.mjs" --explain $_sel_args 2>"$RT_REPORT/select.txt") || _sel_rc=$?
  _q="'"
  if [ "$_sel_rc" != 0 ] ||
    [ "$(printf '%s\n' "$_sel" | wc -l)" != 8 ] ||
    printf '%s\n' "$_sel" | grep -qvE "^(RT_SELECT_ALL=[01]|RT_GO_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_SPECS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_PW_GREP=${_q}[A-Za-z0-9_|()?!^*.: -]*${_q}|RT_GO_CORE_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_GO_CORE_RUN=${_q}[A-Za-z0-9_|^\$()]*${_q}|RT_SPECS_CORE=${_q}[A-Za-z0-9_./ -]*${_q}|RT_KERNEL_TOUCHED=[01])\$"; then
    echo "всё: выбор не удался или его вывод не прошёл проверку (код $_sel_rc)" >>"$RT_REPORT/select.txt"
    RT_SELECT_ALL=1
    RT_SEL_BAD=1
    RT_KERNEL_TOUCHED=1
  else
    eval "$_sel"
  fi
  if [ "$RT_SELECT_ALL" = 1 ]; then
    # полный набор: списки не нужны, этапы go и pw берут значения по умолчанию; списки тестов
    # с меткой ядра и RT_KERNEL_TOUCHED остаются: по ним строится вторая ветвь
    unset RT_GO_PKGS RT_SPECS RT_PW_GREP
    echo "итог: полный набор" >>"$RT_REPORT/select.txt"
  elif [ -z "$RT_GO_PKGS" ] && [ -z "$RT_SPECS" ]; then
    SEL_ANY=0
    echo "итог: затронутого нет — деплой и смоук" >>"$RT_REPORT/select.txt"
  else
    echo "итог: Go: ${RT_GO_PKGS:--}; спеки: ${RT_SPECS:--}" >>"$RT_REPORT/select.txt"
    export RT_GO_PKGS RT_SPECS RT_PW_GREP
  fi
  echo "router-test: выбор — $(tail -n 1 "$RT_REPORT/select.txt")"
fi

# --- план ветвей и окно смоука (D-01…D-03, D-07) -----------------------------------------
# changed без явного RT_CORES — гибрид: первая ветвь идёт под ядром из SHA HEAD со всем
# выбором, вторая (другое ядро) — только с тестами, размеченными как зависящие от ядра, и не
# запускается вовсе, если таких в выборе нет (ни sync, ни переключения, ни смоука, ни окна).
# Полный прогон — полная матрица (оба ядра, все тесты); явный RT_CORES — прежняя семантика
# для ручной отладки. Строки плана — в <отчёт>/plan.txt: раздел «Выбор» report.md и начало
# раздела цели в summary.md.
RT_HYBRID=0
RT_B1=""
RT_B2=""
RT_B2_RUN=0
if [ "$SUITE" = changed ] || [ -z "$SUITE" ]; then
  : >"$RT_REPORT/plan.txt"
  if [ -n "$RT_CORES_SET" ] && [ "$SUITE" = changed ] && [ "$SEL_ANY" = 0 ]; then
    echo "- Ветви: нет (затронутого нет — деплой и смоук; RT_CORES=$RT_CORES не применяется)" >>"$RT_REPORT/plan.txt"
  elif [ -n "$RT_CORES_SET" ]; then
    echo "- Ветви: $RT_CORES (отладка RT_CORES: все выбранные тесты в каждой ветви, гибрид выключен)" >>"$RT_REPORT/plan.txt"
  elif [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ] && [ "$RT_SEL_BAD" = 0 ]; then
    RT_B1=$(rt_sha_core "$(git rev-parse HEAD)")
    case "$RT_B1" in xray) RT_B2=mihomo ;; *) RT_B2=xray ;; esac
    if [ -n "$RT_GO_CORE_PKGS" ] || [ -n "$RT_SPECS_CORE" ]; then
      RT_B2_RUN=1
      RT_CORES="$RT_B1 $RT_B2"
    else
      RT_CORES="$RT_B1"
    fi
    RT_HYBRID=1
    echo "- Ядро ветви: $RT_B1 (правило: cksum SHA HEAD чётный — xray, нечётный — mihomo)" >>"$RT_REPORT/plan.txt"
    if [ "$RT_B2_RUN" = 1 ]; then
      _n_go=0
      if [ -n "$RT_GO_CORE_RUN" ]; then
        _n_go=$(($(printf '%s' "$RT_GO_CORE_RUN" | tr -cd '|' | wc -c) + 1))
      elif [ -n "$RT_GO_CORE_PKGS" ]; then
        # откат на «все роутерные тесты пакетов»: список тестов пуст, пакеты идут целиком
        # shellcheck disable=SC2086
        _n_go="все тесты пакетов ($(printf '%s\n' $RT_GO_CORE_PKGS | grep -c . || true))"
      fi
      # shellcheck disable=SC2086
      _n_pw=$(printf '%s\n' $RT_SPECS_CORE | grep -c . || true)
      echo "- Вторая ветвь ($RT_B2): только тесты с меткой ядра (Go: $_n_go, Playwright: $_n_pw спеков)" >>"$RT_REPORT/plan.txt"
    else
      echo "- Вторая ветвь ($RT_B2): не запускалась — в выборе нет тестов с меткой ядра" >>"$RT_REPORT/plan.txt"
    fi
  elif [ "$SUITE" = changed ] && [ "$RT_SEL_BAD" = 1 ]; then
    echo "- Ветви: $RT_CORES (выбор не прошёл проверку: вторая ветвь со всеми тестами)" >>"$RT_REPORT/plan.txt"
  elif [ "$SUITE" = changed ]; then
    echo "- Ветви: нет (затронутого нет — деплой и смоук)" >>"$RT_REPORT/plan.txt"
  else
    echo "- Ветви: $RT_CORES (полная матрица: все тесты под каждым ядром)" >>"$RT_REPORT/plan.txt"
  fi
  # окно смоука: в changed 120 с при затронутом коде запуска ядра, иначе 30 с; заданный в
  # окружении RT_SMOKE_WINDOW приоритетнее; в полном прогоне — RT_SMOKE_WINDOW или 120 с (D-07)
  if [ "$SUITE" = changed ]; then
    if [ -n "${RT_SMOKE_WINDOW:-}" ]; then
      echo "- Окно смоука: $RT_SMOKE_WINDOW с (RT_SMOKE_WINDOW задан явно)" >>"$RT_REPORT/plan.txt"
    elif [ "$RT_KERNEL_TOUCHED" = 1 ]; then
      RT_SMOKE_WINDOW=120
      echo "- Окно смоука: 120 с (затронут код запуска ядра или выбор полный)" >>"$RT_REPORT/plan.txt"
    else
      RT_SMOKE_WINDOW=30
      echo "- Окно смоука: 30 с (код запуска ядра и watchdog не затронуты)" >>"$RT_REPORT/plan.txt"
    fi
  else
    echo "- Окно смоука: ${RT_SMOKE_WINDOW:-120} с (полный прогон)" >>"$RT_REPORT/plan.txt"
  fi
  [ -z "${RT_SMOKE_WINDOW:-}" ] || export RT_SMOKE_WINDOW
  echo "router-test: ветви — $(head -n 1 "$RT_REPORT/plan.txt")"
fi

# Проверки смоука, которые пропускаются, если цель недоступна или деплой не удался.
SKIP_CHECKS="kernel-running iptables-rules api-version version xcp-pid api-me api-service-status log-panic log-error kernel-restart"

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
    for c in $SKIP_CHECKS; do
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
      for c in $SKIP_CHECKS; do
        rt_result "$t" SKIP "$c" "нет связи"
      done
    else
      rt_result "$t" FAIL deploy "деплой завершился с кодом $drc"
      for c in $SKIP_CHECKS; do
        rt_result "$t" SKIP "$c" "деплой не выполнен"
      done
    fi
  done
fi

# --- смоук параллельно ------------------------------------------------------------

# Полный набор на одной цели: preflight -> снимок -> ветви ядер -> восстановление + сверка.
# Ветвь ядра: переключение через интерфейс панели -> смоук -> Go-тесты -> Playwright.
# Работает в подпроцессе. Восстановление — в EXIT-trap, который ставится сразу после
# успешного снимка: оно выполняется при любом исходе, включая прерывание.
full_target() {
  _t=$1
  _snapped=0
  _orig=""
  # набор changed: этап go или pw пропускается, если выбор его не затронул
  _do_go=1
  _do_pw=1
  if [ "$SUITE" = changed ] && [ "${RT_SELECT_ALL:-1}" != 1 ]; then
    [ -n "${RT_GO_PKGS:-}" ] || _do_go=0
    [ -n "${RT_SPECS:-}" ] || _do_pw=0
  fi
  # Повторный сигнал (TERM группе плюс TERM, пересланный верхним run.sh, или второй Ctrl+C) не
  # должен обрывать восстановление: игнорирование наследуют rt_restore, _after_restore и ssh.
  _finish() {
    trap '' INT TERM HUP
    if [ "$_snapped" = 1 ]; then
      _snapped=0
      # строки восстановления и сверки несут исходное ядро
      RT_CORE=""
      [ -z "$_orig" ] || printf '%s\n' "$_orig" >"$RT_REPORT/$_t/core"
      rt_restore "$_t" || true
      _after_restore "$_t"
    fi
  }
  # Проверка после восстановления: xcp запущен и версия равна установленной деплоем,
  # активное ядро совпадает с исходным из снимка (core-restored, D-19).
  _after_restore() {
    _rc=0
    _out=$(rt_ssh "$1" "/opt/sbin/xcp -v; pidof xcp" 2>/dev/null) || _rc=$?
    _ver=$(printf '%s\n' "$_out" | sed -n '1p')
    _pid=$(printf '%s\n' "$_out" | sed -n '2p')
    if [ "$_rc" = 0 ] && [ -n "$_pid" ] && { [ -z "$EXPECT" ] || [ "$_ver" = "$EXPECT" ]; }; then
      rt_result "$1" PASS xcp-after-restore "xcp запущен, версия $_ver"
    else
      rt_result "$1" FAIL xcp-after-restore "после восстановления xcp не запущен или версия $_ver, ожидалась ${EXPECT:--}"
    fi
    _rc=0
    _now=$(rt_ssh "$1" 'x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); if [ -n "$x" ] && [ -n "$m" ]; then echo both; elif [ -n "$x" ]; then echo xray; elif [ -n "$m" ]; then echo mihomo; else echo none; fi' 2>/dev/null) || _rc=$?
    if [ -z "$_orig" ]; then
      rt_result "$1" FAIL core-restored "исходное ядро не найдено в журнале снимка"
    elif [ "$_rc" = 0 ] && [ "$_now" = "$_orig" ]; then
      rt_result "$1" PASS core-restored "активное ядро $_now равно исходному"
    else
      rt_result "$1" FAIL core-restored "после восстановления ядро ${_now:--}, исходное $_orig"
    fi
  }
  # Ожидание рабочего состояния стенда после разрушающих Go-тестов (остановка, запуск и
  # перезапуск XKeen, D-04): на медленном устройстве перехват и статус XKeen приходят в норму
  # не сразу, а Playwright должен начинать с рабочей панели. Результат — строка stand-ready.
  # Свои имена переменных (_sl_*): sh не имеет локальных, а rt_stage держит _t0 и _rc и
  # по ним пишет код этапа; общие имена портили бы код и обрывали подпроцесс цели под set -e.
  _settle() {
    _sl_t0=$(date +%s)
    _sl_rc=0
    RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" <"$SCRIPT_DIR/settle.sh" >/dev/null 2>&1 || _sl_rc=$?
    _sl_dt=$(($(date +%s) - _sl_t0))
    if [ "$_sl_rc" = 0 ]; then
      rt_result "$1" PASS stand-ready "после Go-тестов ядро запущено и перехват на месте за $_sl_dt с"
    else
      rt_result "$1" FAIL stand-ready "после Go-тестов стенд не пришёл в рабочее состояние за $_sl_dt с (код $_sl_rc)"
    fi
  }
  _sp=""
  # имя и время начала этапа под _bg (см. _bg), время начала наблюдателя окна (шаг 2а)
  _bg_stage=""
  _bg_t0=""
  _ot0=""
  # PID фоновых этапов, идущих одновременно с основным (их запускает план параллельных
  # этапов); _term гасит и их
  _sps=""
  # Остановить этап целиком: он стартует отдельной группой процессов (setsid, PID этапа равен
  # номеру группы), поэтому TERM группе настигает и ssh, и node этапа. Без setsid группы нет —
  # тогда TERM только самому процессу. Запись `kill -TERM -- -N` dash не принимает, форма
  # `kill -s TERM -- -N` работает и в dash, и в bash.
  # Функция оболочки (_settle, rt_ssh) идёт в фоновом подпроцессе без своей группы: ему шлётся
  # TERM, а его дочерние процессы (ssh) собираются заранее и получают TERM отдельно, иначе после
  # смерти подпроцесса они осиротели бы.
  _stop_stage() {
    if kill -s TERM -- "-$1" 2>/dev/null; then
      return 0
    fi
    _st_kids=$(pgrep -P "$1" 2>/dev/null || true)
    kill "$1" 2>/dev/null || true
    for _st_k in $_st_kids; do
      kill -s TERM "$_st_k" 2>/dev/null || true
    done
  }
  # То же с KILL: для этапа, не завершившегося по TERM за отведённый срок.
  _kill_stage() {
    if kill -s KILL -- "-$1" 2>/dev/null; then
      return 0
    fi
    _st_kids=$(pgrep -P "$1" 2>/dev/null || true)
    kill -s KILL "$1" 2>/dev/null || true
    for _st_k in $_st_kids; do
      kill -s KILL "$_st_k" 2>/dev/null || true
    done
  }
  # сигнал: остановить текущий этап целиком и выйти — EXIT-trap восстановит стенд, когда
  # потомки этапа уже не меняют его. Повторный сигнал игнорируется (первая команда): он не
  # входит в _term заново. Оборванные этапы — основной под _bg и наблюдатель окна — пишутся в
  # timings.tsv строкой с кодом 130 в формате rt_stage (цель, этап, секунды, код, начало,
  # ядро): по ним summary.md видит, где оборвался прогон. Что шло, запоминается до остановки.
  _term() {
    trap '' INT TERM HUP
    _tm_stage=""
    _tm_obs=0
    [ -z "$_sp" ] || _tm_stage=$_bg_stage
    [ -z "$_sps" ] || _tm_obs=1
    for _p in $_sp $_sps; do
      _stop_stage "$_p"
    done
    # Ожидание этапов ограничено 60 с: этап, не реагирующий на TERM (ssh к недоступной цели),
    # иначе держал бы подпроцесс цели и блокировку неограниченно, а повторный сигнал игнорируется.
    # Сторож по истечении срока посылает KILL; игнорируемые сигналы он наследует, поэтому его
    # самого и его sleep гасит только KILL.
    (
      sleep 60
      for _p in $_sp $_sps; do
        _kill_stage "$_p"
      done
    ) &
    _tm_kw=$!
    for _p in $_sp $_sps; do
      wait "$_p" 2>/dev/null || true
    done
    pkill -KILL -P "$_tm_kw" 2>/dev/null || true
    kill -s KILL "$_tm_kw" 2>/dev/null || true
    _tm_now=$(date +%s)
    if [ -n "$_tm_stage" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "$_tm_stage" "$((_tm_now - _bg_t0))" 130 "$_bg_t0" "${RT_CORE:--}" >>"$RT_REPORT/timings.tsv"
    fi
    if [ "$_tm_obs" = 1 ] && [ -n "$_ot0" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "observe-$_c" "$((_tm_now - _ot0))" 130 "$_ot0" "${RT_CORE:--}" >>"$RT_REPORT/timings.tsv"
    fi
    exit 130
  }
  # Этап ветви в фоне с ожиданием: код этапа попадает в _brc и возвращается как код функции
  # (вызовы под rt_stage дополняются `|| true`: run.sh идёт под set -e), сигнал прерывает
  # этап целиком. Вызывается только как `rt_stage … _bg …`: rt_stage до вызова кладёт имя
  # этапа в _stage, а время начала — в _t0, и _bg копирует их в свои _bg_stage и _bg_t0 (в sh
  # нет локальных переменных, а rt_stage при восстановлении перезапишет общие): по ним _term
  # пишет строку оборванного этапа. Свои имена — по той же причине, что и _sl_* у _settle.
  _bg() {
    _bg_stage=${_stage:-}
    _bg_t0=${_t0:-}
    # Функцию оболочки (_settle, rt_ssh) setsid запустить не может: она идёт в фоне как есть.
    case "$(type "$1" 2>&1)" in
      *"shell function"* | *"is a function"*) "$@" & ;;
      *)
        if command -v setsid >/dev/null 2>&1; then
          setsid "$@" &
        else
          "$@" &
        fi
        ;;
    esac
    _sp=$!
    _brc=0
    wait "$_sp" || _brc=$?
    _sp=""
    _bg_stage=""
    return "$_brc"
  }
  trap '_finish' EXIT
  trap '_term' INT TERM HUP
  # Флаг ставится до снимка: сигнал, пришедший сразу после возврата ssh снимка, обрабатывается
  # ещё до следующей строки, и без флага восстановление не запустилось бы. Снимок не снят —
  # флаг сбрасывается, восстанавливать нечего.
  _snapped=1
  rt_snapshot "$_t" || {
    _snapped=0
    exit 1
  }
  # Исходное ядро — из журнала снимка (строка core=…): к нему вернёт восстановление.
  _orig=$(sed -n 's/^core=//p' "$RT_REPORT/$_t/snapshot.log" | tail -n 1)

  _first=1
  for _c in $RT_CORES; do
    # Ядро ветви: колонка results.tsv и файл core (селекторы меток arch/core)
    RT_CORE=$_c
    export RT_CORE
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"
    mkdir -p "$RT_REPORT/$_t/$_c"

    # Тесты ветви. Гибрид (changed): в ветви второго ядра идут только тесты с меткой ядра
    # (Go — RT_GO_CORE_PKGS по RT_GO_CORE_RUN, Playwright — RT_SPECS_CORE с отбором @core);
    # пустой список — этап пропускается. Значения этапам передаёт env: _bg запускает этап в
    # фоне отдельной группой процессов. Пустые значения этапы читают как «по умолчанию».
    _b_go=$_do_go
    _b_pw=$_do_pw
    _b_skip="роутерные Go-пакеты набора changed не затронуты"
    _b_skip_pw="роутерные спеки набора changed не затронуты"
    _g_pkgs=${RT_GO_PKGS:-}
    _g_run=${RT_GO_RUN:-}
    _p_specs=${RT_SPECS:-}
    _p_core=""
    if [ "${RT_HYBRID:-0}" = 1 ] && [ "$_c" = "$RT_B2" ]; then
      _g_pkgs=$RT_GO_CORE_PKGS
      _g_run=$RT_GO_CORE_RUN
      _p_specs=$RT_SPECS_CORE
      _p_core=1
      _b_go=1
      _b_pw=1
      [ -n "$RT_GO_CORE_PKGS" ] || _b_go=0
      [ -n "$RT_SPECS_CORE" ] || _b_pw=0
      _b_skip="во второй ветви нет тестов с меткой ядра"
      _b_skip_pw="во второй ветви нет тестов с меткой ядра"
    fi

    # Номер последней строки xcp.log до начала ветви: смоук смотрит только строки после неё,
    # поэтому ошибки, вызванные разрушающими тестами предыдущей ветви (D-04), не
    # засчитываются этой ветви. В первой ветви смоук смотрит от баннера запуска.
    # Вместе с номером строки запоминается inode файла: при размере 1 МБ xcp.log
    # ротируется (RotateWriter: xcp.log -> xcp.log.1 -> xcp.log.2), и без inode номер
    # строки указывал бы уже на другой файл. Смоук по inode находит файл ветви среди
    # xcp.log, .1 и .2 и берёт хвост после запомненной строки (WR-01). Номер inode
    # освобождается после третьей ротации и может достаться новому xcp.log, поэтому
    # запоминается ещё и md5 первой строки: найденный файл с другой первой строкой — не файл ветви.
    _from=""
    _ino=""
    _fp=""
    if [ "$_first" = 0 ]; then
      _lraw=$(rt_ssh "$_t" 'L=/opt/var/log/xcp.log; echo "xcp_lines=$(wc -l <$L)"; set -- $(ls -i $L); echo "xcp_ino=$1"; echo "xcp_fp=$(head -n 1 $L | md5sum | cut -c1-32)"' 2>/dev/null) || _lraw=""
      _from=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_lines=//p' | tr -dc '0-9')
      _ino=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_ino=//p' | tr -dc '0-9')
      _fp=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_fp=//p' | tr -dc '0-9a-f')
    fi
    _first=0

    # 0. Сброс записи на носитель до начала ветви. Установка бинарника, снимок стенда и
    # разрушающие тесты прошлой ветви оставляют в кэше страниц десятки МБ; ядро
    # сбрасывает их примерно через 30 с, и на флешке стенда это застой носителя на
    # десятки секунд — смоук ветви попадал в него: ssh без баннера, выход из панели
    # 36–41 с (гейты 20261020-203115-fdaccf9d, 20261020-214913-b9421f42). sync сбрасывает
    # всё сейчас, до переключения ядра; нет связи — смоук ветви это и покажет.
    rt_stage "$_t" "sync-$_c" _bg rt_ssh "$_t" sync >/dev/null 2>&1 || true

    # 1. переключение ядра интерфейсом панели
    rt_stage "$_t" "switch-$_c" _bg sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --switch "$_c" || true
    _src=$_brc
    if [ "$_src" != 0 ]; then
      for _s in smoke go pw; do
        rt_result "$_t" SKIP "$_s" "переключение на $_c не удалось"
      done
      continue
    fi

    # 2. смоук без окна (только чтение; строки лога — после начала ветви). Ключи --from-* общие
    # для проверок и наблюдателя: числа и шестнадцатеричный отпечаток, кавычки не нужны.
    _fargs=""
    if [ -n "$_from" ] && [ -n "$_ino" ] && [ -n "$_fp" ]; then
      _fargs="--from-line $_from --from-inode $_ino --from-fp $_fp"
    elif [ -n "$_from" ] && [ -n "$_ino" ]; then
      _fargs="--from-line $_from --from-inode $_ino"
    elif [ -n "$_from" ]; then
      _fargs="--from-line $_from"
    fi
    # shellcheck disable=SC2086
    rt_stage "$_t" "smoke-$_c" _bg sh "$SCRIPT_DIR/smoke.sh" "$_t" "$EXPECT" --no-observe $_fargs || true
    _smrc=$_brc
    # смоук записывает в core определённое им ядро: ветвь продолжается с ядром ветви
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"

    # 2а. окно наблюдения (smoke.sh --observe-only: smoke-settle 30 с, окно RT_SMOKE_WINDOW, вердикт
    # kernel-restart и правила логов) фоном параллельно с частью Playwright только чтения
    # (D-07): окно перекрыто обходом страниц, а не простоем. Параллельно окну идут только спеки
    # только чтения; Go-тесты и изменяющие спеки стартуют после ожидания наблюдателя.
    # Цель недоступна (код 3) — наблюдать нечего: смоук уже записал SKIP по этим проверкам.
    if [ "$_smrc" != 3 ]; then
      _ot0=$(date +%s)
      if command -v setsid >/dev/null 2>&1; then
        # shellcheck disable=SC2086
        setsid sh "$SCRIPT_DIR/smoke.sh" "$_t" "$EXPECT" --observe-only $_fargs &
      else
        # shellcheck disable=SC2086
        sh "$SCRIPT_DIR/smoke.sh" "$_t" "$EXPECT" --observe-only $_fargs &
      fi
      _osp=$!
      _sps="$_sps $_osp"
      if [ "$_b_pw" = 1 ]; then
        rt_stage "$_t" "pw-read-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part read || true
      fi
      _orc=0
      wait "$_osp" || _orc=$?
      _sps=""
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "observe-$_c" "$(($(date +%s) - _ot0))" "$_orc" "$_ot0" "$_c" >>"$RT_REPORT/timings.tsv"
    elif [ "$_b_pw" = 1 ]; then
      rt_stage "$_t" "pw-read-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part read || true
    fi
    # хвост xcp.log пишет наблюдатель (без него — смоук без окна не пишет)
    [ ! -f "$RT_REPORT/$_t/xcp-log-tail.txt" ] || mv "$RT_REPORT/$_t/xcp-log-tail.txt" "$RT_REPORT/$_t/$_c/xcp-log-tail.txt"

    # 3. Go-тесты на устройстве с этим ядром
    if [ "$_b_go" = 1 ]; then
      rt_stage "$_t" "go-$_c" _bg env "RT_GO_PKGS=$_g_pkgs" "RT_GO_RUN=$_g_run" sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_c" || true

      # 3а. стенд после разрушающих тестов должен прийти в рабочее состояние
      rt_stage "$_t" "settle-$_c" _bg _settle "$_t" || true
    else
      rt_result "$_t" SKIP go "$_b_skip"
    fi

    # 4. Playwright, часть state: спеки, меняющие состояние стенда, один воркер. Часть только
    # чтения прошла в шаге 2а
    if [ "$_b_pw" = 1 ]; then
      rt_stage "$_t" "pw-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part state || true
    else
      rt_result "$_t" SKIP pw "$_b_skip_pw"
    fi
  done
  RT_CORE=""
  exit 0
}

# --- набор reboot (D-24) -------------------------------------------------------------
# Предполёт и снимок идут параллельно (D-23), перезагрузка — строго по одной цели в порядке
# XCP_REBOOT_ORDER: перезагрузка вышестоящей цели каскада обрывает связь ПК и upstream
# нижестоящей, параллельная перезагрузка дала бы ложные ошибки в xcp.log и недетерминированный
# смоук (D-16). Проверки цели завершаются до перезагрузки следующей.

RB_CHECKS="reboot-back reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify reboot-reachable-end"
TAB=$(printf '\t')

# Метка известного падения проверки перезагрузки: запись `reboot:<проверка>` в known-failures
# (проверка — точно или по префиксу, селектор — по архитектуре и ядру цели). Печатает slug.
rb_label() {
  rb_arch=$(rt_get "$1" ARCH)
  rb_core=-
  [ ! -f "$RT_REPORT/$1/core" ] || rb_core=$(cat "$RT_REPORT/$1/core")
  while IFS="$TAB" read -r rb_lc rb_ls rb_lslug || [ -n "$rb_lc" ]; do
    case "$rb_lc" in reboot:?*) ;; *) continue ;; esac
    rb_p=${rb_lc#reboot:}
    case "$2" in "$rb_p"*) ;; *) continue ;; esac
    rt_selector_match "$rb_ls" "$rb_arch" "$rb_core" || continue
    printf '%s\n' "$rb_lslug"
    return 0
  done <"$SCRIPT_DIR/known-failures"
  return 1
}

# Строка результата: rb_res <цель> <PASS|FAIL|SKIP> <проверка> <деталь>; FAIL с применимой
# меткой становится KNOWN со slug.
rb_res() {
  if [ "$2" = FAIL ]; then
    rb_slug=$(rb_label "$1" "$3") || rb_slug=""
    if [ -n "$rb_slug" ]; then
      rt_result "$1" KNOWN "$3" "$4 [известное падение: $rb_slug]"
      return 0
    fi
  fi
  rt_result "$1" "$2" "$3" "$4"
}

# Остальные проверки цели пропущены: rb_skip <цель> <причина> <проверки…>
rb_skip() {
  rb_sk_t=$1
  rb_sk_why=$2
  shift 2
  for rb_sk_c in "$@"; do
    rt_result "$rb_sk_t" SKIP "$rb_sk_c" "$rb_sk_why"
  done
}

# Снимок на устройстве больше не нужен.
rb_cleanup() {
  rt_ssh "$1" "sh $RT_SNAP_DIR/snapshot.sh cleanup; rm -rf $RT_SNAP_DIR" >/dev/null 2>&1 || true
}

# Состояние автозапуска на устройстве (reboot-state.sh); печатает строки ключ=значение.
# Сразу после холодного старта сеть может моргнуть, поэтому до трёх попыток; вывод считается
# полным, если дошёл до последней строки (nat=). Ошибки ssh — в <отчёт>/<цель>/reboot-state.err.
rb_state() {
  rb_tries=0
  while [ "$rb_tries" -lt 3 ]; do
    rb_out=$(RT_SSH_STDIN=1 rt_ssh "$1" "sh -s${2:+ $2}" <"$SCRIPT_DIR/reboot-state.sh" 2>>"$RT_REPORT/$1/reboot-state.err") || rb_out=""
    if printf '%s\n' "$rb_out" | grep -q '^nat='; then
      printf '%s\n' "$rb_out"
      return 0
    fi
    rb_tries=$((rb_tries + 1))
    sleep 10
  done
  return 1
}

# Ожидание рабочего XKeen после холодного старта (settle.sh), время — в timings.tsv.
rb_settle() {
  rb_s0=$(date +%s)
  RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" <"$SCRIPT_DIR/settle.sh" >/dev/null 2>&1 || true
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" reboot-settle "$(($(date +%s) - rb_s0))" 0 "$rb_s0" - >>"$RT_REPORT/timings.tsv"
}

# Полный цикл одной цели: состояние до -> перезагрузка -> возврат -> состояние после ->
# смоук -> сверка со снимком.
rb_one() {
  rb_t=$1
  rb_b=$(rb_state "$rb_t") || rb_b=""
  printf '%s\n' "$rb_b" >"$RT_REPORT/$rb_t/reboot-state-before.txt"
  rb_bid=$(printf '%s\n' "$rb_b" | rt_kv boot_id)
  rb_bxcp=$(printf '%s\n' "$rb_b" | rt_kv xcp)
  rb_bxk=$(printf '%s\n' "$rb_b" | rt_kv xkeen)
  rb_bcore=$(printf '%s\n' "$rb_b" | rt_kv core)
  rb_bm=$(printf '%s\n' "$rb_b" | rt_kv mangle)
  rb_bn=$(printf '%s\n' "$rb_b" | rt_kv nat)
  rb_start=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if [ -z "$rb_bid" ] || [ -z "$rb_bxcp" ]; then
    rb_res "$rb_t" FAIL reboot-back "не удалось снять состояние до перезагрузки"
    rb_skip "$rb_t" "состояние до перезагрузки не снято" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    rb_cleanup "$rb_t"
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" - "$rb_start" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi
  printf '%s\n' "$rb_bcore" >"$RT_REPORT/$rb_t/core"

  # Команда перезагрузки KeeneticOS из Entware; соединение обрывается — это ожидаемо (код 3)
  rb_rc=0
  rt_ssh "$rb_t" "ndmc -c 'system reboot'" >/dev/null 2>&1 || rb_rc=$?
  if [ "$rb_rc" != 0 ] && [ "$rb_rc" != 3 ]; then
    rb_res "$rb_t" FAIL reboot-back "команда перезагрузки не принята (код $rb_rc)"
    rb_skip "$rb_t" "перезагрузка не выполнена" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    rb_cleanup "$rb_t"
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" - "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi

  rb_rc=0
  rt_stage "$rb_t" reboot-wait rt_wait_back "$rb_t" "$rb_bid" || rb_rc=$?
  rb_back=$RT_BACK_SECS
  if [ "$rb_rc" != 0 ]; then
    rb_res "$rb_t" FAIL reboot-back "нет связи после перезагрузки: за $rb_back с цель не вернулась (снимок остался на устройстве)"
    rb_skip "$rb_t" "нет связи после перезагрузки" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" "$rb_back" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi
  rb_res "$rb_t" PASS reboot-back "вернулась за $rb_back с, загрузка новая"

  [ "$rb_bxk" != 1 ] || rb_settle "$rb_t"
  rb_a=$(rb_state "$rb_t" stable) || rb_a=""
  printf '%s\n' "$rb_a" >"$RT_REPORT/$rb_t/reboot-state-after.txt"
  rb_axcp=$(printf '%s\n' "$rb_a" | rt_kv xcp)
  rb_apid=$(printf '%s\n' "$rb_a" | rt_kv xcp_pid)
  rb_axk=$(printf '%s\n' "$rb_a" | rt_kv xkeen)
  rb_acore=$(printf '%s\n' "$rb_a" | rt_kv core)
  rb_am=$(printf '%s\n' "$rb_a" | rt_kv mangle)
  rb_an=$(printf '%s\n' "$rb_a" | rt_kv nat)
  if [ -z "$rb_a" ]; then
    rb_res "$rb_t" FAIL reboot-xcp "не удалось снять состояние после перезагрузки"
    rb_skip "$rb_t" "состояние после перезагрузки не снято" reboot-xkeen reboot-core reboot-iptables
  else
    # xcp запущен той же версии (S99xcp)
    if [ -n "$rb_apid" ] && [ "$rb_axcp" = "$rb_bxcp" ]; then
      rb_res "$rb_t" PASS reboot-xcp "xcp запущен автозапуском, версия $rb_axcp"
    else
      rb_res "$rb_t" FAIL reboot-xcp "после перезагрузки xcp: PID ${rb_apid:--}, версия ${rb_axcp:--}; до перезагрузки версия $rb_bxcp"
    fi
    # XKeen запущен, если был запущен
    if [ "$rb_bxk" != 1 ]; then
      rb_res "$rb_t" PASS reboot-xkeen "XKeen не работал до перезагрузки — не проверяется"
    elif [ "$rb_axk" = 1 ]; then
      rb_res "$rb_t" PASS reboot-xkeen "XKeen запущен автозапуском"
    else
      rb_res "$rb_t" FAIL reboot-xkeen "XKeen работал до перезагрузки и не запущен после"
    fi
    # то же ядро
    if [ "$rb_acore" = "$rb_bcore" ]; then
      rb_res "$rb_t" PASS reboot-core "ядро $rb_acore запущено, как до перезагрузки"
    else
      rb_res "$rb_t" FAIL reboot-core "после перезагрузки ядро ${rb_acore:--}, до перезагрузки $rb_bcore"
    fi
    # правила XKeen восстановлены
    if [ "$((rb_bm + rb_bn))" = 0 ]; then
      rb_res "$rb_t" PASS reboot-iptables "правил XKeen до перезагрузки не было — не проверяется"
    elif [ "$rb_am" = "$rb_bm" ] && [ "$rb_an" = "$rb_bn" ]; then
      rb_res "$rb_t" PASS reboot-iptables "правила XKeen на месте: mangle $rb_am, nat $rb_an"
    else
      rb_res "$rb_t" FAIL reboot-iptables "правил XKeen mangle ${rb_am:--}, nat ${rb_an:--}; до перезагрузки mangle $rb_bm, nat $rb_bn"
    fi
  fi

  # смоук (D-34): строки лога — с баннера запуска после перезагрузки
  sh "$SCRIPT_DIR/smoke.sh" "$rb_t" "$EXPECT" || true

  # файлы стенда не изменились: сверка со снимком, расхождение — восстановление и падение
  rb_rc=0
  rt_stage "$rb_t" reboot-verify rt_snap_remote "$rb_t" verify >"$RT_REPORT/$rb_t/reboot-verify.log" 2>&1 || rb_rc=$?
  if [ "$rb_rc" = 0 ]; then
    rb_res "$rb_t" PASS reboot-verify "файлы, версии и ядро равны снимку"
    rb_cleanup "$rb_t"
  else
    rb_res "$rb_t" FAIL reboot-verify "расхождение со снимком после перезагрузки (код $rb_rc): $(grep 'ОШИБКА' "$RT_REPORT/$rb_t/reboot-verify.log" | head -n 2)"
    rt_restore "$rb_t" || true
  fi
  printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" "$rb_back" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
}

reboot_suite() {
  # 1. предполёт и снимок параллельно
  for rb_t in ${DEPLOYED:-}; do
    (
      rb_rc=0
      rt_snapshot "$rb_t" || rb_rc=$?
      echo "$rb_rc" >"$RT_REPORT/$rb_t/snap.rc"
    ) &
  done
  wait
  rb_ready=""
  for rb_t in $RT_TARGETS; do
    if [ "$(cat "$RT_REPORT/$rb_t/snap.rc" 2>/dev/null || echo 1)" = 0 ]; then
      rb_ready="$rb_ready $rb_t"
    else
      # shellcheck disable=SC2086
      rb_skip "$rb_t" "цель не готова: нет связи, деплоя или снимка" $RB_CHECKS
    fi
  done

  # 2. порядок: XCP_REBOOT_ORDER, затем цели набора, которых в нём нет
  rb_order=""
  if [ -n "${XCP_REBOOT_ORDER:-}" ]; then
    for rb_o in $XCP_REBOOT_ORDER; do
      case " $rb_ready " in
        *" $rb_o "*) case " $rb_order " in *" $rb_o "*) ;; *) rb_order="$rb_order $rb_o" ;; esac ;;
      esac
    done
  else
    echo "XCP_REBOOT_ORDER не задан: порядок XCP_TARGETS (нижестоящая цель каскада должна идти первой)" >"$RT_REPORT/reboot-warn"
  fi
  for rb_o in $rb_ready; do
    case " $rb_order " in *" $rb_o "*) ;; *) rb_order="$rb_order $rb_o" ;; esac
  done
  rb_order=${rb_order# }
  printf '%s\n' "$rb_order" >"$RT_REPORT/reboot-order"
  : >"$RT_REPORT/reboot.tsv"

  # 3. строго по одной цели
  for rb_t in $rb_order; do
    rb_one "$rb_t"
  done

  # 4. в конце все цели снова отвечают (нижестоящая видна с ПК после перезагрузки вышестоящей)
  for rb_t in $rb_order; do
    rb_rc=0
    rt_stage "$rb_t" reboot-end rt_wait_back "$rb_t" || rb_rc=$?
    if [ "$rb_rc" = 0 ]; then
      rb_res "$rb_t" PASS reboot-reachable-end "отвечает по ssh, панель отвечает (ожидание $RT_BACK_SECS с)"
    else
      rb_res "$rb_t" FAIL reboot-reachable-end "нет связи с целью в конце набора (ожидание $RT_BACK_SECS с)"
    fi
  done

  # 5. метка reboot:* без падения проверки — XPASS (метка больше не нужна)
  for rb_t in $rb_order; do
    rb_arch=$(rt_get "$rb_t" ARCH)
    rb_core=-
    [ ! -f "$RT_REPORT/$rb_t/core" ] || rb_core=$(cat "$RT_REPORT/$rb_t/core")
    while IFS="$TAB" read -r rb_lc rb_ls rb_lslug || [ -n "$rb_lc" ]; do
      case "$rb_lc" in reboot:?*) ;; *) continue ;; esac
      rt_selector_match "$rb_ls" "$rb_arch" "$rb_core" || continue
      rb_p=${rb_lc#reboot:}
      if awk -F'\t' -v p="$rb_p" 'index($3, p) == 1 { if ($1 == "PASS") ok = 1; if ($1 == "KNOWN") k = 1 } END { exit !(ok && !k) }' "$RT_REPORT/$rb_t/results.tsv"; then
        rt_result "$rb_t" XPASS "known:$rb_p" "метка больше не нужна: снять и закрыть todo $rb_lslug"
      fi
    done <"$SCRIPT_DIR/known-failures"
  done
}

PIDS=""
RT_INTR=0
if [ "$SUITE" = reboot ]; then
  reboot_suite
else
  # Сигнал ставится до запуска подпроцессов целей: между запуском и ожиданием он не убивает
  # run.sh и не оставляет цели без ожидания. Под dash сигнал, игнорируемый при старте
  # асинхронного списка (`( full_target ) &`), подпроцесс цели перехватить не может: его
  # trap INT не срабатывает, и Ctrl+C не гасил бы этап. TERM он перехватывает, поэтому
  # обработчик пересылает TERM каждому подпроцессу цели (его PID, не группе: подпроцесс не
  # лидер группы, и TERM группе задел бы сам run.sh). Цель останавливает этап, восстанавливает
  # стенд и выходит с кодом 130.
  trap 'RT_INTR=1; echo "router-test: прерывание — останавливаю этапы и жду восстановления стенда" >&2; for _kp in $PIDS; do kill -s TERM "$_kp" 2>/dev/null || true; done' INT TERM HUP
  for t in ${DEPLOYED:-}; do
    if [ -z "$SUITE" ] || { [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ]; }; then
      (full_target "$t") &
    else
      sh "$SCRIPT_DIR/smoke.sh" "$t" "$EXPECT" &
    fi
    PIDS="$PIDS $!"
  done
fi
# Ждём подпроцессы целей по признаку жизни, а не по коду wait. Прерванный перехваченным
# сигналом wait возвращает больше 128, пока подпроцесс жив; повторный wait завершённого
# подпроцесса в dash и bash снова даёт его сохранённый код, а не 127, поэтому условие по
# величине кода зацикливалось бы навсегда (G-145.1-5). Конец ожидания — kill -0: пока
# подпроцесс жив (в том числе зомби), wait повторяется, и последний wait забирает его код.
# Код 130 — цель прервали напрямую, мимо этого обработчика.
for p in $PIDS; do
  wrc=0
  wait "$p" || wrc=$?
  while kill -0 "$p" 2>/dev/null; do
    wrc=0
    wait "$p" || wrc=$?
  done
  if [ "$wrc" = 130 ]; then
    RT_INTR=1
  fi
done
# На время построения отчёта сигнал не убивает run.sh: он только запоминается (RT_INTR), отчёт
# доводится до конца, и прогон выходит с кодом 130 без удержания блокировки.
trap 'RT_INTR=1' INT TERM HUP

# --- отчёт -------------------------------------------------------------------------

SHA=$(git rev-parse HEAD)
DIRTY=$(sed -n 's/^dirty=//p' "$RT_REPORT/meta")
FAILS=0
XPASSES=0

# Прерванный прогон: пометка в отчётах и список оборванных этапов (строки timings.tsv с кодом
# 130 пишет _term, D-19). Нет таких строк — «-».
if [ "$RT_INTR" = 1 ]; then
  INTR_STAGES=""
  for t in $RT_TARGETS; do
    _is=""
    if [ -f "$RT_REPORT/timings.tsv" ]; then
      _is=$(awk -F'\t' -v t="$t" '$1 == t && $4 == 130 {printf "%s%s/%s", sep, t, $2; sep = ", "}' "$RT_REPORT/timings.tsv") || _is=""
    fi
    [ -z "$_is" ] || INTR_STAGES="${INTR_STAGES:+$INTR_STAGES, }$_is"
  done
fi

# Разделы по ветвям (этапы, восстановление, долгие тесты, метки ядра) строятся до отчётов и
# до подсчёта FAIL: --write дописывает в results.tsv строки FAIL core-mark:… (D-05б, полный
# прогон краснеет на неразмеченном тесте с разным исходом) и пишет <цель>/layout.tsv (D-19).
# Строки плана ветвей (plan.txt) идут первыми в разделе цели.
for t in $RT_TARGETS; do
  mkdir -p "$RT_REPORT/$t"
  node "$SCRIPT_DIR/branch-report.mjs" "$RT_REPORT" "$t" --suite "$SUITE_NAME" --write --plan "$RT_REPORT/plan.txt" >"$RT_REPORT/$t/branch.md" 2>"$RT_REPORT/$t/branch.err" || true
done

{
  echo "# Прогон проверки на роутерах"
  echo
  echo "- Набор: $SUITE_NAME"
  echo "- SHA: $SHA (незакоммиченные правки: $DIRTY)"
  [ "$RT_INTR" != 1 ] || echo "- Прерван: да (сигнал); оборваны этапы: ${INTR_STAGES:--}"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo
  if [ "$SUITE" = changed ] && [ -f "$RT_REPORT/select.txt" ]; then
    echo "## Выбор"
    echo
    echo '```'
    cat "$RT_REPORT/select.txt"
    echo '```'
    echo
    if [ -s "$RT_REPORT/plan.txt" ]; then
      echo "### Ветви"
      echo
      cat "$RT_REPORT/plan.txt"
      echo
    fi
  elif [ -s "$RT_REPORT/plan.txt" ]; then
    echo "## Ветви"
    echo
    cat "$RT_REPORT/plan.txt"
    echo
  fi
  if [ "$SUITE" = reboot ] && [ -f "$RT_REPORT/reboot-order" ]; then
    echo "## Перезагрузки"
    echo
    echo "- Порядок (XCP_REBOOT_ORDER): $(cat "$RT_REPORT/reboot-order")"
    [ ! -f "$RT_REPORT/reboot-warn" ] || echo "- Предупреждение: $(cat "$RT_REPORT/reboot-warn")"
    if [ -s "$RT_REPORT/reboot.tsv" ]; then
      awk -F'\t' '{printf "- %s: начало перезагрузки %s (UTC), возврат через %s с, конец проверок %s (UTC)\n", $1, $2, $3, $4}' "$RT_REPORT/reboot.tsv"
    fi
    echo
  fi
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
    if [ -f "$RT_REPORT/$t/results.tsv" ] && awk -F'\t' '$1 == "KNOWN" {f = 1} END {exit !f}' "$RT_REPORT/$t/results.tsv"; then
      echo "Известные падения (не влияют на код выхода):"
      echo
      awk -F'\t' '$1 == "KNOWN" {printf "- %s: %s\n", $3, $4}' "$RT_REPORT/$t/results.tsv"
      echo
    fi
  done
} >"$RT_REPORT/report.md"

{
  echo "# Сводка прогона"
  echo
  echo "- Набор: $SUITE_NAME"
  echo "- SHA: $SHA"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo "- Проверены цели: $(printf '%s' "$RT_TARGETS" | tr ' ' ',')"
  [ "$RT_INTR" != 1 ] || echo "- Прерван: да (сигнал); оборваны этапы: ${INTR_STAGES:--}"
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
    # план ветвей, этапы по ветвям, итог и критический путь, оболочка, восстановление, долгие
    # тесты, метки ядра (D-19)
    [ ! -f "$RT_REPORT/$t/branch.md" ] || cat "$RT_REPORT/$t/branch.md"
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

# Прерванный прогон блокировку не удерживает: RT_HOLD_MINUTES — для ручной проверки после
# завершённого прогона, а здесь блокировка снимается выходом процесса.
if [ "$RT_INTR" = 1 ]; then
  echo "router-test: прерван (код 130), итог — $RT_REPORT/summary.md" >&2
  exit 130
fi

hold_lock

if [ "$FAILS" -gt 0 ] || [ "$XPASSES" -gt 0 ]; then
  echo "router-test: красный (FAIL: $FAILS, XPASS: $XPASSES)" >&2
  exit 1
fi
echo "router-test: зелёный"
