# scripts/router/lib.sh — общие функции проверки на роутерах.
#
# Подключается через `. "$(dirname "$0")/lib.sh"` из остальных скриптов каталога
# (POSIX sh, у вызывающих `set -eu`). Сам ничего не запускает.
#
# Цели (ssh-алиас, архитектура, URL и пароль панели) и URL подписки читаются из
# неотслеживаемого локального файла основной копии репозитория:
#   scripts/router/targets.local.env      (формат — scripts/router/targets.example.env)
# В git нет ни алиасов, ни адресов, ни паролей: только имена переменных.
#
# Переменные окружения:
#   ROUTERS           выбор целей через пробел или запятую (иначе XCP_TARGETS из файла)
#   XCP_TARGETS_FILE  другой файл целей (иначе локальный файл основной копии)
#   RT_LOCK_WAIT      сколько секунд ждать очередь на роутеры (по умолчанию 3600)
#   RT_LOCK_HELD      1 — блокировка уже взята родительским скриптом
#   RT_REPORT         каталог отчёта текущего прогона (создаёт rt_report_init; этапы принимают его
#                     через rt_report_use: относительный — от корня рабочей копии, вне build/router — код 2)
#   RT_REPORT_DIR     абсолютный путь внутри <корень>/build/router/: отчёт создаётся в нём,
#                     ссылка build/router/last не переставляется (вложенные прогоны сессии);
#                     путь вне build/router — ошибка и код 2
#   RT_CORE           ядро (xray|mihomo) для колонки результатов
#   RT_RESULTS_NAME   имя файла результатов в <отчёт>/<цель>/ (по умолчанию results.tsv; шаблон
#                     [a-z0-9-]+.tsv): перепрогон пишет в тот же отчёт, не смешиваясь с основным проходом

# Корень рабочей копии (основная копия или worktree).
rt_repo_root() {
  git rev-parse --show-toplevel
}

# Общий каталог git: одинаковый для основной копии и всех worktree.
rt_common_dir() {
  git rev-parse --path-format=absolute --git-common-dir
}

# Корень основной копии: работает и из worktree.
rt_main_root() {
  dirname "$(rt_common_dir)"
}

# Путь к локальному файлу целей; нет файла — ошибка с подсказкой.
rt_targets_file() {
  _tf="${XCP_TARGETS_FILE:-$(rt_main_root)/scripts/router/targets.local.env}"
  if [ ! -f "$_tf" ]; then
    echo "router: нет локального файла целей: $_tf" >&2
    echo "router: создайте его по образцу scripts/router/targets.example.env (в git он не попадает)" >&2
    return 1
  fi
  printf '%s\n' "$_tf"
}

# Подключает файл целей и проверяет выбранные цели. Итог: RT_TARGETS — список id.
rt_load_targets() {
  _tf=$(rt_targets_file) || return 1
  # shellcheck disable=SC1090
  . "$_tf"
  _list="${ROUTERS:-${XCP_TARGETS:-}}"
  RT_TARGETS=$(printf '%s' "$_list" | tr ',' ' ' | tr -s ' ' | sed 's/^ //; s/ $//')
  if [ -z "$RT_TARGETS" ]; then
    echo "router: не задан ни ROUTERS, ни XCP_TARGETS в файле целей" >&2
    return 1
  fi
  for _id in $RT_TARGETS; do
    case "$_id" in
      '' | *[!a-z0-9]*)
        echo "router: недопустимый идентификатор цели '$_id' (только a-z и 0-9)" >&2
        return 1
        ;;
    esac
    for _key in SSH ARCH URL PASSWORD; do
      if [ -z "$(rt_get "$_id" "$_key")" ]; then
        echo "router: у цели $_id не задано XCP_T_${_id}_${_key}" >&2
        return 1
      fi
    done
  done
  export RT_TARGETS
}

# Значение XCP_T_<id>_<KEY>. Идентификатор проверен в rt_load_targets.
rt_get() {
  case "$1" in
    '' | *[!a-z0-9]*) return 1 ;;
  esac
  eval "printf '%s' \"\${XCP_T_${1}_${2}:-}\""
}

# ssh на цель. Код 255 (нет связи) превращается в 3. Первая строка удалённой
# команды выставляет PATH Entware, как в обычном входе: иначе tar, curl и прочее
# берутся из прошивки.
rt_ssh() {
  _id=$1
  shift
  _alias=$(rt_get "$_id" SSH)
  _rc=0
  if [ "${RT_SSH_STDIN:-0}" = 1 ]; then
    ssh -o BatchMode=yes -o ConnectTimeout=15 "$_alias" "PATH=/opt/bin:/opt/sbin:\$PATH; export PATH; $*" || _rc=$?
  else
    ssh -n -o BatchMode=yes -o ConnectTimeout=15 "$_alias" "PATH=/opt/bin:/opt/sbin:\$PATH; export PATH; $*" || _rc=$?
  fi
  if [ "$_rc" = 255 ]; then
    echo "цель $_id: нет связи по ssh" >&2
    return 3
  fi
  return "$_rc"
}

# scp на цель: rt_scp <id> <локальный файл> <путь на роутере>. На роутерах нет
# sftp-server, поэтому -O.
rt_scp() {
  _id=$1
  _alias=$(rt_get "$_id" SSH)
  _rc=0
  scp -O -q -o BatchMode=yes -o ConnectTimeout=15 "$2" "$_alias:$3" || _rc=$?
  if [ "$_rc" = 255 ]; then
    echo "цель $_id: нет связи по scp" >&2
    return 3
  fi
  return "$_rc"
}

# Блокировка на весь прогон: общая для основной копии и всех worktree (путь внутри
# общего каталога git). На роутерах flock нет — блокировка только на ПК.
rt_lock() {
  if [ "${RT_LOCK_HELD:-0}" = 1 ]; then
    return 0
  fi
  _lf="$(rt_common_dir)/router-test.lock"
  exec 9>"$_lf"
  if ! flock -n 9; then
    echo "router: очередь на роутеры — занято другим прогоном, жду до ${RT_LOCK_WAIT:-3600} с" >&2
    if ! flock -w "${RT_LOCK_WAIT:-3600}" 9; then
      echo "router: очередь на роутеры не освободилась за ${RT_LOCK_WAIT:-3600} с" >&2
      return 1
    fi
  fi
  RT_LOCK_HELD=1
  export RT_LOCK_HELD
}

# Каталог отчёта: build/router/<UTC дата-время>-<короткий SHA>[-dirty], ссылка last.
rt_report_init() {
  _root=$(rt_repo_root)
  _sha=$(git rev-parse HEAD)
  _short=$(git rev-parse --short HEAD)
  _dirty=""
  _dirty_flag=false
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    _dirty="-dirty"
    _dirty_flag=true
  fi
  _ts=$(date -u +%Y%m%d-%H%M%S)
  _name="$_ts-$_short$_dirty"
  if [ -n "${RT_REPORT_DIR:-}" ]; then
    # вложенный прогон (сессия): отчёт в заданном каталоге, ссылку last не трогаем
    case "$RT_REPORT_DIR" in
      "$_root"/build/router/?*) ;;
      *)
        echo "RT_REPORT_DIR вне build/router" >&2
        return 2
        ;;
    esac
    case "/${RT_REPORT_DIR#"$_root"/build/router/}/" in
      */../* | */./*)
        echo "RT_REPORT_DIR вне build/router" >&2
        return 2
        ;;
    esac
    mkdir -p "$RT_REPORT_DIR"
    RT_REPORT="$RT_REPORT_DIR"
  else
    mkdir -p "$_root/build/router/$_name"
    RT_REPORT="$_root/build/router/$_name"
    ln -sfn "$_name" "$_root/build/router/last"
  fi
  {
    echo "sha=$_sha"
    echo "dirty=$_dirty_flag"
    echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$RT_REPORT/meta"
  export RT_REPORT
}

# Каталог отчёта этапа: принимает RT_REPORT родителя или агента, иначе создаёт новый.
# Относительный путь считается от корня рабочей копии (а не от текущего каталога этапа,
# например frontend/); путь вне build/router или с компонентами «..» и «.» — код 2.
rt_report_use() {
  if [ -z "${RT_REPORT:-}" ]; then
    rt_report_init
    return $?
  fi
  _root=$(rt_repo_root)
  RT_REPORT=${RT_REPORT%/}
  case "$RT_REPORT" in
    /*) ;;
    *) RT_REPORT="$_root/$RT_REPORT" ;;
  esac
  case "$RT_REPORT" in
    "$_root"/build/router/?*) ;;
    *)
      echo "RT_REPORT вне build/router: $RT_REPORT" >&2
      return 2
      ;;
  esac
  case "/${RT_REPORT#"$_root"/build/router/}/" in
    */../* | */./*)
      echo "RT_REPORT вне build/router: $RT_REPORT" >&2
      return 2
      ;;
  esac
  # служебные имена верхнего уровня — не отчёты (кэш релизов, бинарники тестов, ссылки,
  # каталог прогонов без присмотра, история исходов тестов и её индекс)
  _rn=${RT_REPORT#"$_root"/build/router/}
  case "${_rn%%/*}" in
    bin | release | last | session-last | unattended | history.tsv | history.idx)
      echo "RT_REPORT указывает на служебное имя build/router: $RT_REPORT" >&2
      return 2
      ;;
  esac
  mkdir -p "$RT_REPORT"
  export RT_REPORT
}

# Строка результата: rt_result <цель> <PASS|FAIL|KNOWN|XPASS|SKIP> <проверка> <деталь>.
# Формат results.tsv: статус, ядро, проверка, деталь (разделитель — табуляция).
# Файл — <отчёт>/<цель>/${RT_RESULTS_NAME:-results.tsv}; имя вне шаблона [a-z0-9-]+.tsv
# (путь, пробел, другое расширение) отвергается сообщением в stderr, строка уходит в results.tsv.
rt_result() {
  _id=$1
  _st=$2
  _chk=$3
  _det=$(printf '%s' "${4:-}" | tr '\t\n' '  ' | rt_redact)
  mkdir -p "$RT_REPORT/$_id"
  _core="${RT_CORE:-}"
  if [ -z "$_core" ] && [ -f "$RT_REPORT/$_id/core" ]; then
    _core=$(cat "$RT_REPORT/$_id/core")
  fi
  [ -n "$_core" ] || _core="-"
  _rf=${RT_RESULTS_NAME:-results.tsv}
  case "$_rf" in
    *.tsv) _rb=${_rf%.tsv} ;;
    *) _rb="" ;;
  esac
  case "$_rb" in
    '' | *[!a-z0-9-]*)
      echo "rt_result: недопустимое RT_RESULTS_NAME, строка записана в results.tsv" >&2
      _rf=results.tsv
      ;;
  esac
  printf '%s\t%s\t%s\t%s\n' "$_st" "$_core" "$_chk" "$_det" >>"$RT_REPORT/$_id/$_rf"
}

# Ожидание связи со стендом после обрыва: rt_wait_link <цель> <секунды>.
# Раз в 15 с (последний опрос — ровно к сроку) проверяет `rt_ssh <цель> true`. Код 0 — ssh снова
# отвечает, 3 — срок вышел (тот же код, что у rt_ssh при обрыве). Срок не число — одна проверка
# без ожидания. В stderr — строки о начале ожидания и об итоге с секундами.
rt_wait_link() {
  _wl_id=$1
  _wl_max=${2:-0}
  case "$_wl_max" in
    '' | *[!0-9]*) _wl_max=0 ;;
  esac
  _wl_t0=$(date +%s)
  echo "цель $_wl_id: жду связь со стендом до $_wl_max с" >&2
  while :; do
    if rt_ssh "$_wl_id" true >/dev/null 2>&1; then
      echo "цель $_wl_id: связь вернулась через $(($(date +%s) - _wl_t0)) с" >&2
      return 0
    fi
    _wl_el=$(($(date +%s) - _wl_t0))
    if [ "$_wl_el" -ge "$_wl_max" ]; then
      echo "цель $_wl_id: связи нет $_wl_el с, срок $_wl_max с вышел" >&2
      return 3
    fi
    _wl_nap=$((_wl_max - _wl_el))
    [ "$_wl_nap" -le 15 ] || _wl_nap=15
    sleep "$_wl_nap"
  done
}

# Этап с замером времени: rt_stage <цель> <этап> <команда…>. Пишет в timings.tsv
# строку <цель>\t<этап>\t<секунды>\t<код>\t<начало, секунды эпохи>\t<ядро ветви или -> и возвращает
# код команды. Ядро — RT_CORE на момент вызова (общие этапы идут с «-»).
# Время начала нужно, чтобы видеть пересечение этапов разных целей (параллельность).
rt_stage() {
  _id=$1
  _stage=$2
  shift 2
  _t0=$(date +%s)
  _rc=0
  "$@" || _rc=$?
  _t1=$(date +%s)
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_id" "$_stage" "$((_t1 - _t0))" "$_rc" "$_t0" "${RT_CORE:--}" >>"$RT_REPORT/timings.tsv"
  return "$_rc"
}

# Фильтр stdin: убирает из текста адреса, значения ip=, cookie и csrf-токены.
rt_redact() {
  sed -E \
    -e 's/[0-9]{1,3}(\.[0-9]{1,3}){3}/x.x.x.x/g' \
    -e 's/(ip=)[^ ]*/\1***/g' \
    -e 's/([Cc]ookie[^ =:]*[=:] *)[^ ;]*/\1***/g' \
    -e 's/([Cc][Ss][Rr][Ff][^ =:]*[=:] *)[^ ;]*/\1***/g'
}

# Сопоставление селектора метки известного падения с целью:
#   rt_selector_match <селектор> <архитектура> <ядро>
# Грамматика: * | <arch> | <arch>/<ядро> | */<ядро>. Код 0 — селектор подходит.
rt_selector_match() {
  _sel=$1
  _arch=$2
  _core=$3
  case "$_sel" in
    '*') return 0 ;;
    '*/'*) [ "${_sel#*/}" = "$_core" ] ;;
    */*) [ "${_sel%%/*}" = "$_arch" ] && [ "${_sel#*/}" = "$_core" ] ;;
    *) [ "$_sel" = "$_arch" ] ;;
  esac
}

# Каталог снимка на устройстве (создаёт и чистит snapshot.sh).
RT_SNAP_DIR=/opt/tmp/xcp-rt-snap

# Копирует snapshot.sh на устройство и вызывает подкоманду:
#   rt_snap_remote <id> <подкоманда>
# Код возврата — код подкоманды (3 — нет связи).
rt_snap_remote() {
  _id=$1
  _sub=$2
  rt_ssh "$_id" "mkdir -p $RT_SNAP_DIR" || return $?
  rt_scp "$_id" "$(rt_repo_root)/scripts/router/snapshot.sh" "$RT_SNAP_DIR/snapshot.sh" || return $?
  rt_ssh "$_id" "sh $RT_SNAP_DIR/snapshot.sh $_sub"
}

# Предполётная проверка и снимок цели. Пишет FAIL preflight / snapshot в results.tsv.
# Коды: 0 — снимок снят, 1 — нет (прогон цели дальше не идёт), 3 — нет связи.
rt_snapshot() {
  _id=$1
  mkdir -p "$RT_REPORT/$_id"
  _log="$RT_REPORT/$_id/snapshot.log"
  _rc=0
  rt_stage "$_id" preflight rt_snap_remote "$_id" preflight >"$_log" 2>&1 || _rc=$?
  case "$_rc" in
    0) ;;
    3)
      rt_result "$_id" FAIL reachable "нет связи"
      return 3
      ;;
    5 | 6 | 7)
      rt_result "$_id" FAIL preflight "$(tail -n 1 "$_log")"
      return 1
      ;;
    *)
      rt_result "$_id" FAIL preflight "предполётная проверка завершилась с кодом $_rc"
      return 1
      ;;
  esac
  _rc=0
  rt_stage "$_id" snapshot rt_snap_remote "$_id" snapshot >>"$_log" 2>&1 || _rc=$?
  if [ "$_rc" = 3 ]; then
    rt_result "$_id" FAIL reachable "нет связи"
    return 3
  elif [ "$_rc" != 0 ]; then
    rt_result "$_id" FAIL snapshot "снимок не снят (код $_rc): $(grep 'ОШИБКА' "$_log" | head -n 2)"
    return 1
  fi
  rt_result "$_id" PASS snapshot "$(grep '^снимок:' "$_log" | head -n 1)"
}

# Восстановление цели по снимку и сверка. Результат — строка restore-verify:
# PASS, либо FAIL «расхождение после восстановления» (D-19). После успешной
# сверки снимок на устройстве удаляется. Коды: 0 — равно снимку, 1 — нет.
rt_restore() {
  _id=$1
  mkdir -p "$RT_REPORT/$_id"
  _log="$RT_REPORT/$_id/restore.log"
  _rc=0
  rt_stage "$_id" restore rt_snap_remote "$_id" restore >"$_log" 2>&1 || _rc=$?
  if [ "$_rc" = 3 ]; then
    rt_result "$_id" FAIL restore-verify "нет связи, стенд не восстановлен"
    return 1
  elif [ "$_rc" != 0 ]; then
    rt_result "$_id" FAIL restore-verify "расхождение после восстановления (код $_rc): $(grep 'ОШИБКА' "$_log" | head -n 2)"
    return 1
  fi
  _rc=0
  rt_stage "$_id" verify rt_snap_remote "$_id" verify >>"$_log" 2>&1 || _rc=$?
  if [ "$_rc" != 0 ]; then
    rt_result "$_id" FAIL restore-verify "расхождение после восстановления: $(grep 'ОШИБКА' "$_log" | head -n 2)"
    return 1
  fi
  rt_result "$_id" PASS restore-verify "файлы, версии, ядро и xcp равны снимку"
  rt_ssh "$_id" "sh $RT_SNAP_DIR/snapshot.sh cleanup; rm -rf $RT_SNAP_DIR" >/dev/null 2>&1 || true
}

# --- сверка версии стенда на границе единицы работы (D-13, RTR-15) ------------------------------
#
# Файлы отчёта цели (<отчёт>/<цель>/):
#   expect-version    ожидаемая версия — одна строка, как первая строка `/opt/sbin/xcp -v`. Пишут run.sh
#                     после деплоя цели и session.mjs перед этапом; меняет только rt_version_guard при
#                     принятом объявленном переходе (rt_version_set: временный файл и mv -f).
#   version-guard.tsv по строке на границу:
#                       ok<TAB><ядро><TAB><граница><TAB><версия>
#                       transition<TAB><ядро><TAB><граница><TAB><было><TAB><стало>
#                       skip<TAB><ядро><TAB><граница><TAB><причина>
#   version-stop.tsv  stop<TAB><ядро><TAB><граница><TAB><ожидалась><TAB><на стенде><TAB><эпоха ПК> (одна,
#                     первая остановка прогона; в отчёте перепрогона — и прежняя, и новая, граница по
#                     эпохе: RT_VSTOP_SINCE); suspect<TAB><ядро><TAB><проверка> и notrun<TAB><ядро><TAB><что>
#                     дописывает этап (go-suite.sh, pw-suite.sh).
# Читают: run.sh, branch-report.mjs, session.mjs, unattended.sh.
# Граница: go:<каталог пакета>, go:<каталог пакета>#<k> (k-й запуск пакета в RT_GO_RUN_SEQ),
# pw:<спек без tests/router/>.

# Ожидаемая версия цели: rt_version_expected <цель>. Печатает expect-version цели, иначе
# <отчёт>/version (деплой прогона), иначе ничего. Код всегда 0.
rt_version_expected() {
  if [ -s "$RT_REPORT/$1/expect-version" ]; then
    head -n 1 "$RT_REPORT/$1/expect-version"
  elif [ -s "$RT_REPORT/version" ]; then
    head -n 1 "$RT_REPORT/version"
  fi
}

# Новая ожидаемая версия цели: rt_version_set <цель> <версия>. Атомарно: временный файл в том же
# каталоге и mv -f (expect-version читают параллельные этапы и репортёр Playwright).
rt_version_set() {
  _vs_dir="$RT_REPORT/$1"
  mkdir -p "$_vs_dir"
  _vs_tmp=$(mktemp "$_vs_dir/.expect-version.XXXXXX") || return 1
  if printf '%s\n' "$2" >"$_vs_tmp" && mv -f "$_vs_tmp" "$_vs_dir/expect-version"; then
    return 0
  fi
  rm -f "$_vs_tmp"
  return 1
}

# Сверка версии стенда на границе:
#   rt_version_guard <цель> <граница> [<файл объявления на устройстве> <файл parsed запуска на ПК>]
# Одно обращение по ssh читает код и первую строку `/opt/sbin/xcp -v`, а с файлом объявления — ещё
# его первую строку («<версия><TAB><имя теста>», пишет routertest.ExpectVersion) и удаляет его.
# Совпало с ожиданием — строка ok. Не совпало: объявлена ровно наблюдаемая версия, и объявивший
# тест прошёл в этом запуске (строка PASS, KNOWN или QPASS<TAB>go:<тест> в parsed), — новая ожидаемая версия, строка
# transition. Иначе (в том числе код `xcp -v` не 0 при живой связи — «ошибка чтения: код N»)
# остановка: строка stop в version-stop.tsv (если её ещё нет) и строка результата FAIL version-guard.
# Наблюдаемая версия без объявления прошедшего теста новой ожидаемой не становится.
# Нет ожидания — строка skip, связи с цели не требуется.
# Коды: 0 — совпало, переход принят или сверка выключена; 3 — нет связи (строк нет);
# 4 — смена версии, этап останавливается; 2 — неверный вызов.
rt_version_guard() {
  [ $# -eq 2 ] || [ $# -eq 4 ] || {
    echo "rt_version_guard: <цель> <граница> [<объявление> <parsed>]" >&2
    return 2
  }
  _vg_id=$1
  _vg_b=$2
  _vg_decl=${3:-}
  _vg_parsed=${4:-}
  case "$_vg_id" in
    '' | *[!a-z0-9]*)
      echo "rt_version_guard: недопустимая цель" >&2
      return 2
      ;;
  esac
  case "$_vg_b" in
    '' | *[!A-Za-z0-9_.:#/-]*)
      echo "rt_version_guard: недопустимая граница" >&2
      return 2
      ;;
  esac
  # путь объявления встраивается в команду на устройстве: только абсолютный путь без «..»
  case "$_vg_decl" in
    '') ;;
    /*[!A-Za-z0-9_./-]* | */../* | */..)
      echo "rt_version_guard: недопустимый путь объявления" >&2
      return 2
      ;;
    /*) ;;
    *)
      echo "rt_version_guard: путь объявления не абсолютный" >&2
      return 2
      ;;
  esac
  _vg_tab=$(printf '\t')
  _vg_core=${RT_CORE:-}
  [ -n "$_vg_core" ] || _vg_core=-
  _vg_dir="$RT_REPORT/$_vg_id"
  mkdir -p "$_vg_dir"
  _vg_exp=$(rt_version_expected "$_vg_id" | tr -d '\000-\037')
  if [ -z "$_vg_exp" ]; then
    printf 'skip\t%s\t%s\t%s\n' "$_vg_core" "$_vg_b" "ожидаемая версия неизвестна" >>"$_vg_dir/version-guard.tsv"
    return 0
  fi

  _vg_cmd='v=$(/opt/sbin/xcp -v 2>/dev/null); c=$?; printf "rc=%s\n" "$c"; printf "v=%s\n" "$(printf "%s\n" "$v" | head -n 1)"'
  if [ -n "$_vg_decl" ]; then
    _vg_cmd="$_vg_cmd; if [ -f $_vg_decl ]; then printf 'd=%s\n' \"\$(head -n 1 $_vg_decl)\"; fi; rm -f $_vg_decl"
  fi
  _vg_rc=0
  _vg_out=$(rt_ssh "$_vg_id" "$_vg_cmd; exit 0" 2>/dev/null) || _vg_rc=$?
  [ "$_vg_rc" != 3 ] || return 3

  _vg_xrc=$(printf '%s\n' "$_vg_out" | sed -n 's/^rc=//p' | head -n 1)
  if [ "$_vg_rc" != 0 ]; then
    _vg_obs="ошибка чтения: код $_vg_rc"
  elif [ "$_vg_xrc" != 0 ]; then
    _vg_obs="ошибка чтения: код ${_vg_xrc:--}"
  else
    _vg_obs=$(printf '%s\n' "$_vg_out" | sed -n 's/^v=//p' | head -n 1 | tr -d '\000-\037' | sed 's/^ *//; s/ *$//')
    [ -n "$_vg_obs" ] || _vg_obs="ошибка чтения: пустой вывод"
  fi
  if [ "$_vg_obs" = "$_vg_exp" ]; then
    printf 'ok\t%s\t%s\t%s\n' "$_vg_core" "$_vg_b" "$_vg_obs" >>"$_vg_dir/version-guard.tsv"
    return 0
  fi

  # объявленный переход: версия равна наблюдаемой и объявивший тест прошёл в этом запуске
  _vg_dline=$(printf '%s\n' "$_vg_out" | sed -n 's/^d=//p' | head -n 1)
  if [ -n "$_vg_dline" ] && [ -n "$_vg_parsed" ] && [ -f "$_vg_parsed" ]; then
    _vg_dver=$(printf '%s' "${_vg_dline%%"$_vg_tab"*}" | tr -d '\000-\037' | sed 's/^ *//; s/ *$//')
    _vg_dtest=${_vg_dline#*"$_vg_tab"}
    case "$_vg_dline" in *"$_vg_tab"*) ;; *) _vg_dtest="" ;; esac
    case "$_vg_dtest" in
      '' | *[!A-Za-z0-9_./=+,:#-]*) _vg_dtest="" ;;
    esac
    if [ -n "$_vg_dtest" ] && [ -n "$_vg_dver" ] && [ "$_vg_dver" = "$_vg_obs" ] &&
      awk -F'\t' -v n="go:$_vg_dtest" '($1 == "PASS" || $1 == "KNOWN" || $1 == "QPASS") && $2 == n { f = 1 } END { exit !f }' "$_vg_parsed"; then
      if rt_version_set "$_vg_id" "$_vg_obs"; then
        printf 'transition\t%s\t%s\t%s\t%s\n' "$_vg_core" "$_vg_b" "$_vg_exp" "$_vg_obs" >>"$_vg_dir/version-guard.tsv"
        return 0
      fi
    fi
  fi

  # одна строка stop на прогон: строки прежних прогонов того же отчёта (перепрогон пишет в отчёт FROM)
  # не в счёт — у них эпоха ПК раньше RT_VSTOP_SINCE (его задаёт run.sh при старте цели)
  if ! awk -F'\t' -v s="${RT_VSTOP_SINCE:-0}" '$1 == "stop" && $6 + 0 >= s + 0 { f = 1 } END { exit !f }' "$_vg_dir/version-stop.tsv" 2>/dev/null; then
    printf 'stop\t%s\t%s\t%s\t%s\t%s\n' "$_vg_core" "$_vg_b" "$_vg_exp" "$_vg_obs" "$(date +%s)" >>"$_vg_dir/version-stop.tsv"
  fi
  case "$_vg_obs" in
    "ошибка чтения"*)
      rt_result "$_vg_id" FAIL version-guard "версия стенда не прочитана: ожидалась $_vg_exp, на стенде: $_vg_obs (граница $_vg_b)"
      ;;
    *)
      rt_result "$_vg_id" FAIL version-guard "версия стенда сменилась: ожидалась $_vg_exp, на стенде $_vg_obs (граница $_vg_b)"
      ;;
  esac
  return 4
}

# Значение ключа из строк «ключ=значение»: rt_kv <ключ> (текст на stdin).
rt_kv() {
  sed -n "s/^$1=//p" | head -n 1
}

# Ждёт возврата цели после перезагрузки: ssh отвечает и панель отвечает 200 на /api/version.
#   rt_wait_back <id> [boot_id_до]
# С boot_id ждёт ещё и его смены: пока устройство не выключилось, ответ старой загрузки не
# считается возвратом. Опрос раз в 10 с до RT_REBOOT_WAIT (по умолчанию 900; mipsle медленнее).
# Итог: RT_BACK_SECS — сколько секунд прошло до возврата или до выхода срока;
# код 0 — вернулась, 1 — срок вышел (D-17).
rt_wait_back() {
  _wb_id=$1
  _wb_boot=${2:-}
  _wb_max=${RT_REBOOT_WAIT:-900}
  _wb_url=$(rt_get "$_wb_id" URL)
  _wb_t0=$(date +%s)
  RT_BACK_SECS=0
  while :; do
    _wb_bid=$(rt_ssh "$_wb_id" 'cat /proc/sys/kernel/random/boot_id' 2>/dev/null) || _wb_bid=""
    if [ -n "$_wb_bid" ] && { [ -z "$_wb_boot" ] || [ "$_wb_bid" != "$_wb_boot" ]; }; then
      _wb_code=$(curl -sk --connect-timeout 5 --max-time 10 -o /dev/null -w '%{http_code}' "$_wb_url/api/version" 2>/dev/null) || _wb_code=000
      if [ "$_wb_code" = 200 ]; then
        RT_BACK_SECS=$(($(date +%s) - _wb_t0))
        return 0
      fi
    fi
    RT_BACK_SECS=$(($(date +%s) - _wb_t0))
    if [ "$RT_BACK_SECS" -ge "$_wb_max" ]; then
      return 1
    fi
    sleep 10
  done
}
