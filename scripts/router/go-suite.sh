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
# Тесты получают окружение XCP_RT_ARCH, XCP_RT_CORE, XCP_RT_WORKDIR, XCP_RT_PANEL (адрес
# панели на loopback устройства), XCP_RT_SESSION_FILE (cookie и CSRF, права 600) и
# XCP_RT_LINKS (данные share-ссылок internal/nodes/testdata/links).
#
# Окружение:
#   RT_GO_PKGS    пакеты через пробел (по умолчанию все с роутерными тестами)
#   RT_GO_RUN     регэксп -test.run (по умолчанию ^TestRouter): отладка одного теста
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

URL=$(rt_get "$ID" URL)
JAR="$RT_REPORT/$ID/.go-cookies"
SESSION_LOCAL="$RT_REPORT/$ID/.go-session.env"
CSRF=""

# При любом исходе: сессия панели закрывается выходом с ПК (лимит 20 сессий),
# файлы сессии на ПК удаляются, рабочий каталог на устройстве (в нём и session.env)
# убирается целиком.
cleanup() {
  if [ -n "$CSRF" ] && [ -f "$JAR" ]; then
    curl -sk --connect-timeout 10 --max-time 20 -b "$JAR" -X POST -H "X-CSRF-Token: $CSRF" -o /dev/null "$URL/api/auth/logout" || true
  fi
  rm -f "$JAR" "$SESSION_LOCAL"
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

# --- сессия панели и данные ссылок -----------------------------------------------
# Вход делается с ПК: пароль на устройство не передаётся, тело запроса строится через
# JSON.stringify, пароль идёт через окружение и stdin. На устройство уходят только cookie
# и CSRF-токен в файле с правами 600; тесты читают их из XCP_RT_SESSION_FILE.
PORT=$(printf '%s' "$URL" | sed -n 's|^[a-z]*://[^/:]*:\([0-9]*\).*|\1|p')
PANEL="https://127.0.0.1:${PORT:-8090}"
SESSION_ENV=""
rm -f "$JAR"
body=$(RT_PW="$(rt_get "$ID" PASSWORD)" node -e 'process.stdout.write(JSON.stringify({ password: process.env.RT_PW }))' || true)
lcode=$(printf '%s' "$body" | curl -sk --connect-timeout 10 --max-time 20 -c "$JAR" -H 'Content-Type: application/json' --data-binary @- -o /dev/null -w '%{http_code}' "$URL/api/auth/login" || true)
body=""
if [ "$lcode" = 200 ]; then
  me=$(curl -sk --connect-timeout 10 --max-time 20 -b "$JAR" "$URL/api/auth/me" || true)
  CSRF=$(printf '%s\n' "$me" | sed -n 's/.*"csrf_token" *: *"\([^"]*\)".*/\1/p' | head -n 1)
  cookie=$(awk -F'\t' '$6 ~ /xcp_session/ {print $6 "=" $7; exit}' "$JAR")
  if [ -n "$CSRF" ] && [ -n "$cookie" ]; then
    (
      umask 077
      printf 'XCP_RT_COOKIE=%s\nXCP_RT_CSRF=%s\n' "$cookie" "$CSRF" >"$SESSION_LOCAL"
    )
    chmod 600 "$SESSION_LOCAL"
    rc=0
    rt_scp "$ID" "$SESSION_LOCAL" "$RWD/session.env" || rc=$?
    if [ "$rc" = 0 ]; then
      rt_ssh "$ID" "chmod 600 $RWD/session.env" || rc=$?
    fi
    if [ "$rc" = 0 ]; then
      SESSION_ENV="XCP_RT_PANEL=$PANEL XCP_RT_SESSION_FILE=$RWD/session.env"
    elif [ "$rc" = 3 ]; then
      rt_result "$ID" FAIL reachable "нет связи"
      exit 3
    fi
  fi
  cookie=""
fi
if [ -z "$SESSION_ENV" ]; then
  rt_result "$ID" FAIL "go:session" "сессия панели для Go-тестов не создана (вход: http ${lcode:--})"
  BAD=$((BAD + 1))
fi

# Данные share-ссылок для тестов: одним копированием каталога.
LINKS_ENV=""
if [ -d "$ROOT/internal/nodes/testdata/links" ]; then
  rc=0
  scp -O -q -r -o BatchMode=yes -o ConnectTimeout=15 "$ROOT/internal/nodes/testdata/links" "$(rt_get "$ID" SSH):$RWD/links" || rc=$?
  if [ "$rc" = 255 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    LINKS_ENV="XCP_RT_LINKS=$RWD/links"
  else
    rt_result "$ID" FAIL "go:links" "копирование данных ссылок не удалось (код $rc)"
    BAD=$((BAD + 1))
  fi
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
  # Запуск идёт через небольшой скрипт: рядом с тестом работает опрос VmHWM (пиковая
  # память тестового бинарника), последняя строка попадает в go-<имя>.hwm отчёта.
  runner="$RT_REPORT/$ID/.go-run.sh"
  cat >"$runner" <<EOF
cd $RWD
XCP_RT_ARCH=$ARCH XCP_RT_CORE=$CORE XCP_RT_WORKDIR=$RWD $SESSION_ENV $LINKS_ENV ./$name.test -test.v=test2json -test.run '${RT_GO_RUN:-^TestRouter}' -test.timeout 20m &
T=\$!
( while kill -0 \$T 2>/dev/null; do grep VmHWM /proc/\$T/status > $RWD/$name.hwm 2>/dev/null; sleep 1; done ) &
W=\$!
wait \$T
rc=\$?
kill \$W 2>/dev/null
exit \$rc
EOF
  rc=0
  rt_scp "$ID" "$runner" "$RWD/run-$name.sh" || rc=$?
  rm -f "$runner"
  if [ "$rc" = 3 ]; then
    LOST=1
    break
  fi
  rc=0
  rt_stage "$ID" "go-run-$name" rt_ssh "$ID" "sh $RWD/run-$name.sh" >"$raw" 2>"$errlog" || rc=$?
  if [ "$rc" = 3 ]; then
    LOST=1
    rt_result "$ID" FAIL "go:$name" "связь потеряна во время запуска тестов"
    break
  fi
  # stderr устройства: адреса и токены скрыты
  rt_redact <"$errlog" >"$errlog.red" && mv "$errlog.red" "$errlog"

  rt_ssh "$ID" "cat $RWD/$name.hwm" >"$RT_REPORT/$ID/go-$name.hwm" 2>/dev/null || true
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
