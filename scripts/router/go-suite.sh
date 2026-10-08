#!/bin/sh
# scripts/router/go-suite.sh <id цели> <ядро> — Go-тесты на самом устройстве (тег router).
#
# Для каждого пакета с файлами `//go:build router` в `_test.go`:
#   1. на ПК собирается тестовый бинарник под архитектуру цели
#      (`go test -c -tags router`, CGO_ENABLED=0, для mipsle GOMIPS=softfloat);
#   2. бинарник копируется на устройство в /opt/tmp/xcp-rt и запускается там
#      (`-test.v=test2json -test.run '^TestRouter'`), по одному за раз;
#   3. вывод разбирается `go tool test2json` и превращается в строки результатов
#      `go:<Тест>` в <отчёт>/<id>/results.tsv: PASS, FAIL, SKIP, а также KNOWN и XPASS
#      по маркерам `KNOWN-FAILURE slug=` и `XPASS slug=` (метки известных падений).
# Рабочий каталог на устройстве удаляется после этапа при любом исходе.
#
# Окружение:
#   RT_GO_PKGS    пакеты через пробел (по умолчанию все с роутерными тестами)
#   RT_REPORT     каталог отчёта (если не задан, создаётся новый)
#   ROUTERS       игнорируется: цель задана первым аргументом
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна, иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

[ $# -ge 1 ] || {
  echo "usage: go-suite.sh <id цели> [ядро]" >&2
  exit 2
}
ID=$1
CORE=${2:-}
[ "$CORE" != "-" ] || CORE=""

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
[ -n "${RT_REPORT:-}" ] || rt_report_init
mkdir -p "$RT_REPORT/$ID"
RT_CORE=$CORE
export RT_CORE

ARCH=$(rt_get "$ID" ARCH)
RWD=/opt/tmp/xcp-rt
ROOT=$(rt_repo_root)
BIN_DIR="$ROOT/build/router/bin/$ARCH"
mkdir -p "$BIN_DIR"
BAD=0
LOST=0

# Рабочий каталог на устройстве убирается целиком при любом исходе.
cleanup() {
  rt_ssh "$ID" "rm -rf $RWD" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Список пакетов: RT_GO_PKGS или все каталоги, где у _test.go первая строка `//go:build router`.
if [ -n "${RT_GO_PKGS:-}" ]; then
  PKGS=$RT_GO_PKGS
else
  PKGS=""
  for f in $(find internal cmd -name '*_test.go' 2>/dev/null | sort); do
    if [ "$(head -n 1 "$f")" = "//go:build router" ]; then
      d=$(dirname "$f")
      case " $PKGS " in
        *" $d "*) ;;
        *) PKGS="$PKGS $d" ;;
      esac
    fi
  done
  PKGS=${PKGS# }
fi
if [ -z "$PKGS" ]; then
  rt_result "$ID" SKIP "go:suite" "нет пакетов с тегом router"
  exit 0
fi

# Сборка под архитектуру цели.
build_pkg() {
  _pkg=$1
  _out=$2
  case "$ARCH" in
    mipsle | mips) _mips=softfloat ;;
    *) _mips="" ;;
  esac
  CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH GOMIPS=$_mips \
    go test -c -tags router -ldflags='-s -w' -o "$_out" "./$_pkg"
}

# Разбор потока test2json в строки «статус<TAB>проверка<TAB>деталь» (go-parse.awk).
parse_jsonl() {
  awk -v pkg="$1" -f "$SCRIPT_DIR/go-parse.awk"
}

TAB=$(printf '\t')

# --- каталог на устройстве -----------------------------------------------------
rc=0
rt_ssh "$ID" "rm -rf $RWD; mkdir -p $RWD; chmod 700 $RWD" || rc=$?
if [ "$rc" != 0 ]; then
  if [ "$rc" = 3 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  fi
  rt_result "$ID" FAIL "go:setup" "не удалось создать $RWD на устройстве (код $rc)"
  exit 1
fi

# --- пакеты --------------------------------------------------------------------
for pkg in $PKGS; do
  name=$(printf '%s' "${pkg#internal/}" | tr '/' '-')
  bin="$BIN_DIR/$name.test"
  buildlog="$RT_REPORT/$ID/go-$name.build.log"

  rc=0
  rt_stage "$ID" "go-build-$name" build_pkg "$pkg" "$bin" >"$buildlog" 2>&1 || rc=$?
  if [ "$rc" != 0 ]; then
    rt_result "$ID" FAIL "go:build:$name" "сборка под $ARCH не удалась: $(tail -n 3 "$buildlog" | tr '\n' ' ')"
    BAD=$((BAD + 1))
    continue
  fi

  rc=0
  rt_scp "$ID" "$bin" "$RWD/$name.test" || rc=$?
  if [ "$rc" = 3 ]; then
    LOST=1
    break
  elif [ "$rc" != 0 ]; then
    rt_result "$ID" FAIL "go:upload:$name" "копирование бинарника на устройство не удалось (код $rc)"
    BAD=$((BAD + 1))
    continue
  fi

  raw="$RT_REPORT/$ID/go-$name.raw"
  jsonl="$RT_REPORT/$ID/go-$name.jsonl"
  errlog="$RT_REPORT/$ID/go-$name.stderr"
  rc=0
  rt_stage "$ID" "go-run-$name" rt_ssh "$ID" "cd $RWD && XCP_RT_ARCH=$ARCH XCP_RT_CORE=$CORE XCP_RT_WORKDIR=$RWD ./$name.test -test.v=test2json -test.run '^TestRouter' -test.timeout 20m" >"$raw" 2>"$errlog" || rc=$?
  if [ "$rc" = 3 ]; then
    LOST=1
    rt_result "$ID" FAIL "go:$name" "связь потеряна во время запуска тестов"
    break
  fi
  # stderr устройства: адреса и токены скрыты
  rt_redact <"$errlog" >"$errlog.red" && mv "$errlog.red" "$errlog"

  go tool test2json -t -p "$pkg" <"$raw" >"$jsonl"
  found=0
  parse_jsonl "$pkg" <"$jsonl" >"$RT_REPORT/$ID/go-$name.parsed"
  while IFS="$TAB" read -r st chk det || [ -n "$st" ]; do
    found=$((found + 1))
    rt_result "$ID" "$st" "$chk" "$det"
    case "$st" in FAIL | XPASS) BAD=$((BAD + 1)) ;; esac
  done <"$RT_REPORT/$ID/go-$name.parsed"
  if [ "$found" = 0 ]; then
    rt_result "$ID" FAIL "go:$name" "тесты не найдены или бинарник не запустился (код $rc): $(tail -n 2 "$errlog" | tr '\n' ' ')"
    BAD=$((BAD + 1))
  elif [ "$rc" != 0 ] && [ "$BAD" = 0 ]; then
    rt_result "$ID" FAIL "go:$name" "бинарник завершился с кодом $rc без упавших тестов"
    BAD=$((BAD + 1))
  fi
  rm -f "$raw"
done

if [ "$LOST" = 1 ]; then
  rt_result "$ID" FAIL reachable "нет связи в ходе Go-тестов"
  exit 3
fi
[ "$BAD" = 0 ] || exit 1
exit 0
