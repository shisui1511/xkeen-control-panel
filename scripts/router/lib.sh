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
#   RT_REPORT         каталог отчёта текущего прогона (создаёт rt_report_init)
#   RT_CORE           ядро (xray|mihomo) для колонки результатов

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
  mkdir -p "$_root/build/router/$_name"
  RT_REPORT="$_root/build/router/$_name"
  ln -sfn "$_name" "$_root/build/router/last"
  {
    echo "sha=$_sha"
    echo "dirty=$_dirty_flag"
    echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$RT_REPORT/meta"
  export RT_REPORT
}

# Строка результата: rt_result <цель> <PASS|FAIL|KNOWN|XPASS|SKIP> <проверка> <деталь>.
# Формат results.tsv: статус, ядро, проверка, деталь (разделитель — табуляция).
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
  printf '%s\t%s\t%s\t%s\n' "$_st" "$_core" "$_chk" "$_det" >>"$RT_REPORT/$_id/results.tsv"
}

# Этап с замером времени: rt_stage <цель> <этап> <команда…>. Пишет в timings.tsv
# строку <цель>\t<этап>\t<секунды>\t<код> и возвращает код команды.
rt_stage() {
  _id=$1
  _stage=$2
  shift 2
  _t0=$(date +%s)
  _rc=0
  "$@" || _rc=$?
  _t1=$(date +%s)
  printf '%s\t%s\t%s\t%s\n' "$_id" "$_stage" "$((_t1 - _t0))" "$_rc" >>"$RT_REPORT/timings.tsv"
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
