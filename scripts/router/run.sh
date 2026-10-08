#!/bin/sh
# scripts/router/run.sh [SUITE] — проверка на роутерах (make router-test SUITE=...).
#
# Наборы:
#   (без SUITE)  полный: деплой, снимок, затем матрица ядер (D-20) — для каждого ядра
#                из RT_CORES ветвь «переключение ядра через интерфейс панели
#                (pw-suite.sh --switch) -> смоук по строкам лога после начала ветви ->
#                Go-тесты на устройстве (go-suite.sh) -> Playwright против панели
#                (pw-suite.sh)»; затем восстановление, сверка и проверка core-restored.
#   smoke        деплой и смоук (только чтение, без снимка).
#   changed      быстрый набор для каждой задачи (D-25): select-changed.mjs по diff от
#                origin/main и незакоммиченным правкам выбирает Go-пакеты и роутерные спеки;
#                дальше та же матрица ядер, что у полного, но go и pw — только выбранное.
#                Ничего не затронуто — деплой и смоук (без снимка). Причины выбора —
#                <отчёт>/select.txt и раздел «Выбор» отчёта.
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
# при любом исходе, в том числе при падении этапов посередине.
#
# Окружение:
#   ROUTERS=<id,id>      выбор целей (по умолчанию все из конфига)
#   RELEASE=<тег>        ставить артефакт GitHub Release вместо сборки ветки
#   RT_HOLD_MINUTES=<N>  после прогона держать блокировку N минут для ручной
#                        проверки; снять раньше: touch <git-common-dir>/router-test.release
#   RT_LOCK_WAIT=<с>     ожидание очереди на роутеры (по умолчанию 3600)
#   RT_SMOKE_WINDOW=<с>  окно наблюдения смоука за перезапуском ядра (по умолчанию 120)
#   RT_CORES="<ядра>"    ветви матрицы через пробел (по умолчанию «xray mihomo»); сужать
#                        можно для ручной отладки, гейт перед PR — обе ветви на обеих целях
#   RT_REBOOT_WAIT=<с>   ожидание возврата цели после перезагрузки (по умолчанию 900)
#   XCP_REBOOT_ORDER     порядок перезагрузки (локальный конфиг, id через пробел)
#   RT_BASE=<ref>        база сравнения набора changed (по умолчанию merge-base с origin/main)
#   RT_FILES=<a,b,c>     набор changed: выбор для заданного списка файлов вместо git (отладка)
#
# Раскладка отчёта: результаты всех ветвей — в <отчёт>/<id>/results.tsv (ядро ветви — во
# второй колонке), артефакты ветви (go-*.jsonl, pw/, pw-switch/) — в <отчёт>/<id>/<ядро>/.
#
# Код выхода: 0 — нет строк FAIL и XPASS, 2 — неизвестный набор, иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

SUITE=${1:-}
case "$SUITE" in
  smoke | changed | reboot | '') ;;
  *)
    echo "router-test: неизвестный набор '${SUITE}'. Поддерживаются: smoke, changed, reboot и полный (без SUITE)" >&2
    echo "router-test: пример: make router-test SUITE=smoke" >&2
    exit 2
    ;;
esac
SUITE_NAME=${SUITE:-full}

# Ветви матрицы ядер: только xray и mihomo, без повторов.
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

cd "$(rt_repo_root)"

# Метки известных падений должны ссылаться на живые todo (D-13): до блокировки и деплоя.
if ! sh "$SCRIPT_DIR/check-known.sh"; then
  echo "router-test: красный — метки известных падений ссылаются на закрытые или несуществующие todo" >&2
  exit 1
fi

rt_load_targets
rt_lock
rt_report_init
echo "router-test: отчёт $RT_REPORT"

# --- выбор проверок набора changed (D-25) ------------------------------------------
# select-changed.mjs печатает четыре строки для eval; перед eval вывод проверяется по
# строгому шаблону (T-144.1-23), при любом сомнении выбирается всё.
SEL_ANY=1
if [ "$SUITE" = changed ]; then
  _sel_args=""
  [ -z "${RT_BASE:-}" ] || _sel_args="--base $RT_BASE"
  [ -z "${RT_FILES:-}" ] || _sel_args="--files $RT_FILES"
  _sel_rc=0
  # shellcheck disable=SC2086
  _sel=$(node "$SCRIPT_DIR/select-changed.mjs" --explain $_sel_args 2>"$RT_REPORT/select.txt") || _sel_rc=$?
  _q="'"
  if [ "$_sel_rc" != 0 ] ||
    [ "$(printf '%s\n' "$_sel" | wc -l)" != 4 ] ||
    printf '%s\n' "$_sel" | grep -qvE "^(RT_SELECT_ALL=[01]|RT_GO_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_SPECS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_PW_GREP=${_q}[A-Za-z0-9_|()?!^*.: -]*${_q})\$"; then
    echo "всё: выбор не удался или его вывод не прошёл проверку (код $_sel_rc)" >>"$RT_REPORT/select.txt"
    RT_SELECT_ALL=1
  else
    eval "$_sel"
  fi
  if [ "$RT_SELECT_ALL" = 1 ]; then
    # полный набор: списки не нужны, этапы go и pw берут значения по умолчанию
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
  _finish() {
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
  _settle() {
    _t0=$(date +%s)
    _rc=0
    RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" <"$SCRIPT_DIR/settle.sh" >/dev/null 2>&1 || _rc=$?
    _dt=$(($(date +%s) - _t0))
    if [ "$_rc" = 0 ]; then
      rt_result "$1" PASS stand-ready "после Go-тестов ядро запущено и перехват на месте за $_dt с"
    else
      rt_result "$1" FAIL stand-ready "после Go-тестов стенд не пришёл в рабочее состояние за $_dt с (код $_rc)"
    fi
  }
  _sp=""
  # сигнал: остановить текущий этап и выйти — EXIT-trap восстановит стенд
  _term() {
    [ -z "$_sp" ] || kill "$_sp" 2>/dev/null || true
    exit 130
  }
  # Этап ветви в фоне с ожиданием: код этапа попадает в _brc, сигнал прерывает этап.
  _bg() {
    "$@" &
    _sp=$!
    _brc=0
    wait "$_sp" || _brc=$?
    _sp=""
  }
  trap '_finish' EXIT
  trap '_term' INT TERM HUP
  rt_snapshot "$_t" || exit 1
  _snapped=1
  # Исходное ядро — из журнала снимка (строка core=…): к нему вернёт восстановление.
  _orig=$(sed -n 's/^core=//p' "$RT_REPORT/$_t/snapshot.log" | tail -n 1)

  _first=1
  for _c in $RT_CORES; do
    # Ядро ветви: колонка results.tsv и файл core (селекторы меток arch/core)
    RT_CORE=$_c
    export RT_CORE
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"
    mkdir -p "$RT_REPORT/$_t/$_c"

    # Номер последней строки xcp.log до начала ветви: смоук смотрит только строки после неё,
    # поэтому ошибки, вызванные разрушающими тестами предыдущей ветви (D-04), не
    # засчитываются этой ветви. В первой ветви смоук смотрит от баннера запуска.
    _from=""
    if [ "$_first" = 0 ]; then
      _from=$(rt_ssh "$_t" 'wc -l </opt/var/log/xcp.log' 2>/dev/null | tr -dc '0-9') || _from=""
    fi
    _first=0

    # 1. переключение ядра интерфейсом панели
    rt_stage "$_t" "switch-$_c" _bg sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --switch "$_c"
    _src=$_brc
    if [ "$_src" != 0 ]; then
      for _s in smoke go pw; do
        rt_result "$_t" SKIP "$_s" "переключение на $_c не удалось"
      done
      continue
    fi

    # 2. смоук (только чтение; строки лога — после начала ветви)
    if [ -n "$_from" ]; then
      _bg sh "$SCRIPT_DIR/smoke.sh" "$_t" "$EXPECT" --from-line "$_from"
    else
      _bg sh "$SCRIPT_DIR/smoke.sh" "$_t" "$EXPECT"
    fi
    # смоук записывает в core определённое им ядро: ветвь продолжается с ядром ветви
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"
    [ ! -f "$RT_REPORT/$_t/xcp-log-tail.txt" ] || mv "$RT_REPORT/$_t/xcp-log-tail.txt" "$RT_REPORT/$_t/$_c/xcp-log-tail.txt"

    # 3. Go-тесты на устройстве с этим ядром
    if [ "$_do_go" = 1 ]; then
      rt_stage "$_t" "go-$_c" _bg sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_c"

      # 3а. стенд после разрушающих тестов должен прийти в рабочее состояние
      _settle "$_t"
    else
      rt_result "$_t" SKIP go "роутерные Go-пакеты набора changed не затронуты"
    fi

    # 4. Playwright против настоящей панели: цели идут параллельно своими процессами
    # (D-23), внутри цели — один воркер
    if [ "$_do_pw" = 1 ]; then
      rt_stage "$_t" "pw-$_c" _bg sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c"
    else
      rt_result "$_t" SKIP pw "роутерные спеки набора changed не затронуты"
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
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" reboot-settle "$(($(date +%s) - rb_s0))" 0 "$rb_s0" >>"$RT_REPORT/timings.tsv"
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
if [ "$SUITE" = reboot ]; then
  reboot_suite
else
  for t in ${DEPLOYED:-}; do
    if [ -z "$SUITE" ] || { [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ]; }; then
      (full_target "$t") &
    else
      sh "$SCRIPT_DIR/smoke.sh" "$t" "$EXPECT" &
    fi
    PIDS="$PIDS $!"
  done
fi
# Фоновые подпроцессы игнорируют SIGINT, поэтому прерывание run.sh не обрывает
# восстановление: wait, прерванный сигналом (код больше 128), повторяем, пока
# подпроцесс не завершится (повторный wait завершённого даёт 127).
trap 'echo "router-test: прерывание — жду восстановления стенда" >&2' INT TERM HUP
for p in $PIDS; do
  while :; do
    wrc=0
    wait "$p" || wrc=$?
    if [ "$wrc" -le 128 ] || [ "$wrc" = 127 ]; then
      break
    fi
  done
done
trap - INT TERM HUP

# --- отчёт -------------------------------------------------------------------------

SHA=$(git rev-parse HEAD)
DIRTY=$(sed -n 's/^dirty=//p' "$RT_REPORT/meta")
FAILS=0
XPASSES=0

{
  echo "# Прогон проверки на роутерах"
  echo
  echo "- Набор: $SUITE_NAME"
  echo "- SHA: $SHA (незакоммиченные правки: $DIRTY)"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo
  if [ "$SUITE" = changed ] && [ -f "$RT_REPORT/select.txt" ]; then
    echo "## Выбор"
    echo
    echo '```'
    cat "$RT_REPORT/select.txt"
    echo '```'
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
