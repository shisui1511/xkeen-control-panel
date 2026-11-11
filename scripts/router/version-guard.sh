#!/bin/sh
# scripts/router/version-guard.sh <id цели> <граница> — сверка версии стенда на границе единицы
# работы (D-13, RTR-15): CLI над rt_version_guard из lib.sh.
#
# Сравнивает первую строку `/opt/sbin/xcp -v` цели с ожидаемой версией (<отчёт>/<id>/expect-version,
# иначе <отчёт>/version; нет обоих — строка skip, сверка выключена) и пишет строку в
# <отчёт>/<id>/version-guard.tsv; при смене — строку stop в <отчёт>/<id>/version-stop.tsv и строку
# результата FAIL version-guard. Объявления теста здесь нет: Go-тесты объявляют версию через
# go-suite.sh, граница спека — без объявления.
#
# Граница: pw:<имя файла спека без tests/router/> (переключение ядра — pw:core-switch.spec.ts) или
# go:<каталог пакета>[#<k>]; символы — [A-Za-z0-9_.:#/-]. Цель — [a-z0-9]+.
#
# Окружение (обязательно): RT_REPORT — каталог отчёта прогона (принимается через rt_report_use:
# относительный — от корня рабочей копии, вне build/router — код 2), RT_CORE — ядро ветви для
# колонки ядра (xray, mihomo или «-»).
#
# Код выхода: 0 — версия совпала или сверка выключена; 3 — нет связи; 4 — смена версии стенда,
# этап останавливается; 2 — неверный вызов.
#
# Потребитель: репортёр Playwright frontend/tests/router/lib/version-reporter.ts (после каждого
# спека) и ручные этапы.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

if [ $# -ne 2 ]; then
  echo "usage: version-guard.sh <id цели> <граница>" >&2
  exit 2
fi
case "$1" in
  '' | *[!a-z0-9]*)
    echo "version-guard: недопустимый идентификатор цели (только a-z и 0-9)" >&2
    exit 2
    ;;
esac
case "$2" in
  '' | *[!A-Za-z0-9_.:#/-]*)
    echo "version-guard: недопустимая граница (только A-Z, a-z, 0-9 и _.:#/-)" >&2
    exit 2
    ;;
esac
if [ -z "${RT_REPORT:-}" ] || [ -z "${RT_CORE+x}" ]; then
  echo "version-guard: нужны RT_REPORT и RT_CORE в окружении" >&2
  exit 2
fi

cd "$(rt_repo_root)"
ROUTERS=$1
export ROUTERS
rt_load_targets || exit 2
rt_report_use || exit 2

rc=0
rt_version_guard "$1" "$2" || rc=$?
exit "$rc"
