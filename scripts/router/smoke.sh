#!/bin/sh
# scripts/router/smoke.sh <id цели> <ожидаемая версия xcp> — смоук цели после деплоя.
#
# Проверки (каждая пишет строку в <отчёт>/<id>/results.tsv):
#   version      /opt/sbin/xcp -v равна ожидаемой версии
#   xcp-pid      у pidof xcp ровно один процесс
#   api-version  GET <панель>/api/version с ПК отвечает 200
#
# Код выхода: 0 — нет FAIL, 3 — цель недоступна (reachable), иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

if [ $# -ne 2 ]; then
  echo "usage: smoke.sh <id цели> <ожидаемая версия>" >&2
  exit 2
fi
ID=$1
EXPECT=$2

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
[ -n "${RT_REPORT:-}" ] || rt_report_init

FAILED=0

skip_rest() {
  for c in "$@"; do
    rt_result "$ID" SKIP "$c" "нет связи"
  done
}

# --- version -----------------------------------------------------------------
rc=0
got=$(rt_stage "$ID" smoke-version rt_ssh "$ID" "/opt/sbin/xcp -v") || rc=$?
if [ "$rc" = 3 ]; then
  rt_result "$ID" FAIL reachable "нет связи по ssh"
  skip_rest version xcp-pid api-version
  exit 3
elif [ "$rc" != 0 ]; then
  rt_result "$ID" FAIL version "xcp -v завершился с кодом $rc"
  FAILED=1
elif [ "$got" = "$EXPECT" ]; then
  rt_result "$ID" PASS version "$got"
  printf '%s\n' "$got" >"$RT_REPORT/$ID/xcp_version"
else
  rt_result "$ID" FAIL version "на цели $got, ожидалась $EXPECT"
  printf '%s\n' "$got" >"$RT_REPORT/$ID/xcp_version"
  FAILED=1
fi

# --- xcp-pid -----------------------------------------------------------------
rc=0
pids=$(rt_stage "$ID" smoke-pid rt_ssh "$ID" "pidof xcp") || rc=$?
if [ "$rc" = 3 ]; then
  rt_result "$ID" FAIL reachable "нет связи по ssh"
  skip_rest xcp-pid api-version
  exit 3
fi
count=$(printf '%s' "$pids" | wc -w | tr -d ' ')
if [ "$rc" = 0 ] && [ "$count" = 1 ]; then
  rt_result "$ID" PASS xcp-pid "1 процесс"
else
  rt_result "$ID" FAIL xcp-pid "процессов xcp: $count"
  FAILED=1
fi

# --- api-version -------------------------------------------------------------
url=$(rt_get "$ID" URL)
t0=$(date +%s)
code=$(curl -sk --connect-timeout 10 --max-time 20 -o /dev/null -w '%{http_code}' "$url/api/version" || true)
t1=$(date +%s)
printf '%s\t%s\t%s\t%s\n' "$ID" smoke-api "$((t1 - t0))" 0 >>"$RT_REPORT/timings.tsv"
if [ "$code" = 200 ]; then
  rt_result "$ID" PASS api-version "http 200"
elif [ "$code" = 000 ] || [ -z "$code" ]; then
  rt_result "$ID" FAIL api-version "нет связи с панелью"
  FAILED=1
else
  rt_result "$ID" FAIL api-version "http $code"
  FAILED=1
fi

[ "$FAILED" = 0 ] || exit 1
exit 0
