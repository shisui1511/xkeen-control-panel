#!/bin/sh
# scripts/router/rtenv.sh <id цели> write | clear [--no-restart] | status — блок сокращённых сроков
# xcp-rt в /opt/etc/xcp/xcp.env цели (D-16 фазы 148.1.1, D-20…D-23).
#
#   write                 записать блок значений таблицы RT_RTENV_VALUES (lib.sh): блок совпал — ничего
#                         (unchanged), иначе перезапуск панели и ожидание её ответа (written)
#   clear [--no-restart]  снять блок (cleared) или сообщить, что его нет (absent); с --no-restart без
#                         перезапуска панели (её перезапустит деплой)
#   status                строки блока или absent
#
# Тонкая обёртка над rt_rtenv_write, rt_rtenv_clear и rt_rtenv_status (lib.sh) для сессии (session.mjs)
# и шагов поиска виновного коммита (tree-step.sh). Блокировку не берёт и RT_LOCK_HELD не проверяет
# (как go-suite.sh): вызывается только внутри живой блокировки прогона или сессии.
#
# Коды: 0 — сделано, 1 — сбой (сценарий на устройстве упал, панель не поднялась, цели не загружены),
# 2 — неверный вызов, 3 — нет связи.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

usage() {
  echo "usage: rtenv.sh <id цели> write | clear [--no-restart] | status" >&2
  exit 2
}

[ $# -ge 2 ] || usage
ID=$1
MODE=$2
shift 2
NO_RESTART=""
case "$MODE" in
  write | status)
    [ $# -eq 0 ] || usage
    ;;
  clear)
    if [ $# -eq 1 ] && [ "$1" = --no-restart ]; then
      NO_RESTART=--no-restart
    elif [ $# -ne 0 ]; then
      usage
    fi
    ;;
  *) usage ;;
esac
case "$ID" in
  '' | *[!a-z0-9]*) usage ;;
esac

cd "$SCRIPT_DIR"
cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets || exit 1

rc=0
case "$MODE" in
  write) rt_rtenv_write "$ID" || rc=$? ;;
  clear) rt_rtenv_clear "$ID" $NO_RESTART || rc=$? ;;
  status) rt_rtenv_status "$ID" || rc=$? ;;
esac
case "$rc" in
  0 | 3) exit "$rc" ;;
  *) exit 1 ;;
esac
