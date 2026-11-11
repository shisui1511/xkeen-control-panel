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
# XCP_RT_LINKS (данные share-ссылок internal/nodes/testdata/links), XCP_RT_INPUTS
# (входные тела ответов internal/nodes/testdata/inputs), XCP_RT_CONTRACT (контракт
# ключей ответов API источников frontend/src/lib/sourcesContract.json),
# XCP_RT_ROUTING_CONTRACT (то же для маршрутизации, routingContract.json), XCP_RT_SETUP
# (копия scripts/setup.sh этой сборки: тесты запускают её во временном каталоге) и, если в
# локальном конфиге целей задан XCP_SUBSCRIPTION_URL, XCP_RT_SUBSCRIPTION_URL (адрес
# настоящей подписки: передаётся файлом с правами 600, в журнал и отчёт не попадает).
#
# Окружение:
#   RT_GO_PKGS    пакеты через пробел (по умолчанию все с роутерными тестами)
#   RT_GO_RUN     регэксп -test.run (по умолчанию ^TestRouter): отладка одного теста и вторая
#                 ветвь набора changed (только тесты с меткой ядра, RT_GO_CORE_RUN выбора);
#                 одинарная кавычка и перевод строки недопустимы (строка FAIL go:setup)
#   RT_GO_RUN_SEQ упорядоченный список `<каталог пакета>:<TestRouterИмя>` через пробел
#                 (например `internal/handlers:TestRouterA internal/handlers:TestRouterB`);
#                 каждая запись — по шаблону ^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$, иначе
#                 строка FAIL go:setup и выход 1. Задан — RT_GO_PKGS и RT_GO_RUN не используются
#                 (сообщение в stderr): пакеты — каталоги записей в порядке первого появления,
#                 каждый собирается и копируется на устройство один раз, подряд идущие записи
#                 одного пакета — один запуск `-test.run '^(A|B)$'`, запуски идут в порядке списка
#                 (порядок гейта по частоте падений, дешёвые тесты первыми в сессии)
#   RT_ART_SUB    подкаталог артефактов ветви ([a-z0-9-]+, иначе FAIL go:setup): артефакты —
#                 в <отчёт>/<id>/<ядро>/<RT_ART_SUB>/ (перепрогон в тот же отчёт не затирает
#                 основной проход)
#   RT_RESULTS_NAME  имя файла результатов вместо results.tsv (lib.sh, rt_result)
#   RT_REPORT     каталог отчёта (если не задан, создаётся новый); относительный — от корня
#                 рабочей копии, вне build/router — код 2
#   ROUTERS       игнорируется: цель задана первым аргументом
#
# Артефакты этапа (go-<пакет>.jsonl, .build.log, .stderr, .parsed, .hwm) лежат в
# <отчёт>/<id>/<ядро>/[<RT_ART_SUB>/] (матрица ядер, D-20); строки результатов — в
# <отчёт>/<id>/results.tsv (или RT_RESULTS_NAME). Этапы времени — go-build-<пакет> и go-run-<пакет>.
# С RT_GO_RUN_SEQ сборка та же (go-<пакет>.build.log, этап go-build-<пакет>), а у k-го запуска
# пакета (k от 1 по пакету) — этап go-run-<пакет>-<k> и файлы go-<пакет>-<k>.raw, .jsonl, .parsed,
# .stderr, .hwm. Строка результата провала запуска — go:<пакет>, в детали — номер запуска и тесты.
#
# Потребители файлов этапа: results.tsv — run.sh, branch-report.mjs, session.mjs; go-*.jsonl в
# <ядро>/ — branch-report.mjs (самые долгие тесты; go-<пакет>-<k>.jsonl под шаблон попадает);
# go-*.raw в <ядро>/ — триггер interrupt и сторож остановки в session.mjs (в режиме
# последовательности — go-<пакет>-<k>.raw; проходы сессии идут через RT_GO_RUN_SEQ); этапы go-build-*, go-run-* в
# timings.tsv — подэтапы, таблица branch-report.mjs их не берёт; go-done.txt — догон класса stand.
# Файлы в <ядро>/<RT_ART_SUB>/ эти обходы не видят: подкаталог читает тот, кто его задал.
#
# go-done.txt в каталоге артефактов: по строке (каталог пакета) на пакет, обработка которого
# закончилась без обрыва связи — все запуски пакета прошли, либо сборка или копирование не
# удались по другой причине (повтор после обрыва их не изменит). Файл обнуляется в начале этапа;
# пакеты вне него догоняются после возврата связи (класс stand).
#
# Падения считаются на запуск: PKG_BAD (FAIL и XPASS) и PKG_QFAIL (QFAIL — упавший тест в
# карантине, в BAD не входит и код этапа не меняет). Строка «бинарник завершился с кодом …
# без упавших тестов» пишется только при PKG_BAD=0 и PKG_QFAIL=0: прежний общий счётчик BAD
# прятал её, если упал любой предыдущий пакет.
#
# Сверка версии стенда (D-13, RTR-15): после каждого запуска группы (пакет целиком или k-й запуск
# пакета в RT_GO_RUN_SEQ) rt_version_guard (lib.sh) сравнивает первую строку `/opt/sbin/xcp -v` с
# ожидаемой версией цели (<отчёт>/<id>/expect-version, иначе <отчёт>/version; нет обоих — строка
# skip, сверка выключена). Граница — go:<каталог пакета> или go:<каталог пакета>#<k>. Тест, который
# оставляет стенд на другой версии, объявляет её файлом $XCP_RT_WORKDIR/expect-version
# (routertest.ExpectVersion); скрипт запуска группы удаляет этот файл перед тестами, сверка —
# после чтения. Переход принимается, только если объявивший тест прошёл в этой группе.
# Файлы отчёта цели: version-guard.tsv (ok, transition, skip — по строке на границу) и
# version-stop.tsv (stop — первая остановка; suspect<TAB><ядро><TAB><проверка> — FAIL и XPASS
# группы, на которой сменилась версия, и её строка go:<пакет>; notrun<TAB><ядро><TAB>Go: <каталог
# пакета>: <выражение запуска> — группы, которые не запускались). Потребители: run.sh,
# branch-report.mjs, session.mjs, unattended.sh. go-done.txt и догон класса stand не меняются.
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна, 4 — смена версии стенда (этап
# остановлен на границе группы), иначе 1. Код 3 — только потеря связи.

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
rt_report_use
mkdir -p "$RT_REPORT/$ID"
RT_CORE=$CORE
export RT_CORE
ART="$RT_REPORT/$ID/${CORE:-unknown}"
# Подкаталог артефактов ветви: только [a-z0-9-]+ (имя становится путём, T-148.1-07).
if [ -n "${RT_ART_SUB:-}" ]; then
  case "$RT_ART_SUB" in
    *[!a-z0-9-]*)
      rt_result "$ID" FAIL "go:setup" "RT_ART_SUB с недопустимыми символами"
      exit 1
      ;;
  esac
  ART="$ART/$RT_ART_SUB"
fi
mkdir -p "$ART"
: >"$ART/go-done.txt"

# RT_GO_RUN встраивается в скрипт запуска на устройстве в одинарных кавычках (T-145.1-19):
# одинарная кавычка или перевод строки вывели бы значение из кавычек. Проверка — до всякой
# работы со стендом.
case "${RT_GO_RUN:-}" in
  *"'"* | *"
"*)
    rt_result "$ID" FAIL "go:setup" "RT_GO_RUN с недопустимыми символами"
    exit 1
    ;;
esac

# RT_GO_RUN_SEQ: каждая запись — <каталог пакета>:TestRouter<Имя> из [A-Za-z0-9_./-] и
# [A-Za-z0-9_] (T-148.1-06): регэксп -test.run собирается только из проверенных имён.
# Разбиение без раскрытия шаблонов имён файлов (set -f). Итог — GROUPS: строки
# «пакет<TAB>A|B» по группам подряд идущих записей одного пакета, в порядке списка.
TAB=$(printf '\t')
GROUPS=""
if [ -n "${RT_GO_RUN_SEQ:-}" ]; then
  set -f
  _g_pkg=""
  _g_run=""
  _seq_bad=""
  for _e in $RT_GO_RUN_SEQ; do
    _d=${_e%%:*}
    _t=${_e#*:}
    case "$_e" in *:*) ;; *) _seq_bad=$_e ;; esac
    case "$_d" in '' | *[!A-Za-z0-9_./-]*) _seq_bad=$_e ;; esac
    case "$_t" in TestRouter?*) ;; *) _seq_bad=$_e ;; esac
    case "${_t#TestRouter}" in *[!A-Za-z0-9_]*) _seq_bad=$_e ;; esac
    _d=${_d#./}
    [ -n "$_d" ] || _seq_bad=$_e
    [ -z "$_seq_bad" ] || break
    if [ "$_d" = "$_g_pkg" ]; then
      _g_run="$_g_run|$_t"
    else
      [ -z "$_g_pkg" ] || GROUPS="$GROUPS$_g_pkg$TAB$_g_run
"
      _g_pkg=$_d
      _g_run=$_t
    fi
  done
  set +f
  if [ -n "$_seq_bad" ]; then
    rt_result "$ID" FAIL "go:setup" "RT_GO_RUN_SEQ: запись не по шаблону <каталог пакета>:TestRouter<Имя>"
    exit 1
  fi
  [ -z "$_g_pkg" ] || GROUPS="$GROUPS$_g_pkg$TAB$_g_run
"
  if [ -n "${RT_GO_PKGS:-}" ] || [ -n "${RT_GO_RUN:-}" ]; then
    echo "go-suite: задан RT_GO_RUN_SEQ — RT_GO_PKGS и RT_GO_RUN не используются" >&2
  fi
fi

ARCH=$(rt_get "$ID" ARCH)
RWD=/opt/tmp/xcp-rt
ROOT=$(rt_repo_root)
BIN_DIR="$ROOT/build/router/bin/$ARCH"
mkdir -p "$BIN_DIR"
BAD=0
LOST=0
VSTOP=0

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
  rm -f "$JAR" "$SESSION_LOCAL" "${SUB_LOCAL:-}"
  rt_ssh "$ID" "rm -rf $RWD" >/dev/null 2>&1 || true
}
trap cleanup EXIT
# Прерывание сигналом: dash не выполняет EXIT-trap, когда процесс гибнет от необработанного
# сигнала; выход из обработчика запускает cleanup (выход из панели, файлы сессии и подписки
# на ПК, рабочий каталог на устройстве).
trap 'exit 130' INT TERM HUP

# Список пакетов: каталоги записей RT_GO_RUN_SEQ, RT_GO_PKGS или все каталоги, где у _test.go
# первая строка `//go:build router`. Без RT_GO_RUN_SEQ группа запусков — одна на пакет.
SEQ=0
if [ -n "$GROUPS" ]; then
  SEQ=1
  PKGS=$(printf '%s' "$GROUPS" | cut -f1 | awk '!seen[$0]++' | tr '\n' ' ')
  PKGS=${PKGS% }
elif [ -n "${RT_GO_PKGS:-}" ]; then
  # select-changed.mjs выдаёт пути вида ./internal/pkg
  PKGS=""
  for d in $RT_GO_PKGS; do
    PKGS="$PKGS ${d#./}"
  done
  PKGS=${PKGS# }
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
if [ "$SEQ" = 0 ]; then
  for pkg in $PKGS; do
    GROUPS="$GROUPS$pkg$TAB
"
  done
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
# Данные для тестов (этот блок и два следующих): новый путь данных, который тесты получают
# через окружение, надо добавить в TEST_DATA_ALL в scripts/router/select-changed.mjs, иначе
# SUITE=changed его правку не увидит (граф зависимостей Go такие связи не показывает).
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

# Входные тела ответов (Clash YAML, Xray-JSON, sing-box, wg.conf) для тестов разбора.
INPUTS_ENV=""
if [ -d "$ROOT/internal/nodes/testdata/inputs" ]; then
  rc=0
  scp -O -q -r -o BatchMode=yes -o ConnectTimeout=15 "$ROOT/internal/nodes/testdata/inputs" "$(rt_get "$ID" SSH):$RWD/inputs" || rc=$?
  if [ "$rc" = 255 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    INPUTS_ENV="XCP_RT_INPUTS=$RWD/inputs"
  else
    rt_result "$ID" FAIL "go:inputs" "копирование входных данных не удалось (код $rc)"
    BAD=$((BAD + 1))
  fi
fi

# Контракт ключей ответов API источников: тесты сверяют с ним ответы панели.
CONTRACT_ENV=""
if [ -f "$ROOT/frontend/src/lib/sourcesContract.json" ]; then
  rc=0
  rt_scp "$ID" "$ROOT/frontend/src/lib/sourcesContract.json" "$RWD/sourcesContract.json" || rc=$?
  if [ "$rc" = 3 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    CONTRACT_ENV="XCP_RT_CONTRACT=$RWD/sourcesContract.json"
  else
    rt_result "$ID" FAIL "go:contract" "копирование контракта ответов не удалось (код $rc)"
    BAD=$((BAD + 1))
  fi
fi

# Контракт ключей ответов API маршрутизации: так же, отдельным файлом.
ROUTING_CONTRACT_ENV=""
if [ -f "$ROOT/frontend/src/lib/routingContract.json" ]; then
  rc=0
  rt_scp "$ID" "$ROOT/frontend/src/lib/routingContract.json" "$RWD/routingContract.json" || rc=$?
  if [ "$rc" = 3 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    ROUTING_CONTRACT_ENV="XCP_RT_ROUTING_CONTRACT=$RWD/routingContract.json"
  else
    rt_result "$ID" FAIL "go:routing-contract" "копирование контракта ответов маршрутизации не удалось (код $rc)"
    BAD=$((BAD + 1))
  fi
fi

# Установщик этой сборки: тесты запускают настоящий setup.sh на устройстве.
SETUP_ENV=""
if [ -f "$ROOT/scripts/setup.sh" ]; then
  rc=0
  rt_scp "$ID" "$ROOT/scripts/setup.sh" "$RWD/setup.sh" || rc=$?
  if [ "$rc" = 3 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    SETUP_ENV="XCP_RT_SETUP=$RWD/setup.sh"
  else
    rt_result "$ID" FAIL "go:setup-script" "копирование setup.sh не удалось (код $rc)"
    BAD=$((BAD + 1))
  fi
fi

# Адрес настоящей подписки из локального конфига целей: на устройство уходит файлом
# с правами 600 внутри рабочего каталога (он удаляется при выходе), в окружение теста
# значение подставляет сам запускающий скрипт на устройстве. Адрес нигде не печатается.
SUB_ENV=""
if [ -n "${XCP_SUBSCRIPTION_URL:-}" ]; then
  SUB_LOCAL="$RT_REPORT/$ID/.go-subscription.url"
  (
    umask 077
    printf '%s' "$XCP_SUBSCRIPTION_URL" >"$SUB_LOCAL"
  )
  rc=0
  rt_scp "$ID" "$SUB_LOCAL" "$RWD/subscription.url" || rc=$?
  rm -f "$SUB_LOCAL"
  if [ "$rc" = 0 ]; then
    rc=0
    rt_ssh "$ID" "chmod 600 $RWD/subscription.url" || rc=$?
  fi
  if [ "$rc" = 3 ]; then
    rt_result "$ID" FAIL reachable "нет связи"
    exit 3
  elif [ "$rc" = 0 ]; then
    # одинарные кавычки: $(cat ...) выполнится на устройстве, а не здесь
    SUB_ENV='XCP_RT_SUBSCRIPTION_URL=$(cat '"$RWD"'/subscription.url)'
    # HWID устройства: у провайдера занят один слот, посторонний HWID получает заглушки
    # (all_stubs), поэтому проверки с настоящей подпиской идут с HWID самой панели.
    SUB_ENV="$SUB_ENV"' XCP_RT_HWID=$(cat /opt/etc/xcp/data/xcp_hwid.txt 2>/dev/null)'
  else
    rt_result "$ID" FAIL "go:subscription" "передача адреса подписки не удалась (код $rc)"
    BAD=$((BAD + 1))
  fi
fi

# --- пакеты --------------------------------------------------------------------
# Группы запусков идут в порядке GROUPS. Пакет собирается и копируется на устройство при первой
# своей группе (PREPARED — бинарник на устройстве, FAILED — сборка или копирование не удались:
# остальные группы пакета пропускаются). После последней группы пакета без обрыва связи его
# каталог дописывается в go-done.txt. Строки GROUPS разбиваются по переводу строки без
# раскрытия шаблонов; в теле цикла IFS и раскрытие прежние.
PREPARED=" "
FAILED=" "
RUNS=""
_gi=0
_ifs=$IFS
IFS='
'
set -f
for _grp in $GROUPS; do
  IFS=$_ifs
  set +f
  _gi=$((_gi + 1))
  pkg=${_grp%%"$TAB"*}
  grun=${_grp#*"$TAB"}
  name=$(printf '%s' "${pkg#internal/}" | tr '/' '-')
  bin="$BIN_DIR/$name.test"
  ready=1

  case "$FAILED" in *" $pkg "*) ready=0 ;; esac
  case "$PREPARED" in
    *" $pkg "*) ;;
    *)
      if [ "$ready" = 1 ]; then
        buildlog="$ART/go-$name.build.log"
        rc=0
        rt_stage "$ID" "go-build-$name" build_pkg "$pkg" "$bin" >"$buildlog" 2>&1 || rc=$?
        if [ "$rc" != 0 ]; then
          rt_result "$ID" FAIL "go:build:$name" "сборка под $ARCH не удалась: $(tail -n 3 "$buildlog" | tr '\n' ' ')"
          BAD=$((BAD + 1))
          ready=0
        else
          rc=0
          rt_scp "$ID" "$bin" "$RWD/$name.test" || rc=$?
          if [ "$rc" = 3 ]; then
            LOST=1
            break
          elif [ "$rc" != 0 ]; then
            rt_result "$ID" FAIL "go:upload:$name" "копирование бинарника на устройство не удалось (код $rc)"
            BAD=$((BAD + 1))
            ready=0
          fi
        fi
        if [ "$ready" = 1 ]; then
          PREPARED="$PREPARED$pkg "
        else
          FAILED="$FAILED$pkg "
        fi
      fi
      ;;
  esac

  if [ "$ready" = 1 ]; then
    # Имена файлов и этапа: без RT_GO_RUN_SEQ — <пакет>, с ним — <пакет>-<k> (k-й запуск пакета).
    if [ "$SEQ" = 1 ]; then
      k=$(($(printf '%s' "$RUNS" | grep -cxF -e "$pkg" || true) + 1))
      RUNS="$RUNS$pkg
"
      tag="$name-$k"
      runre="^($grun)\$"
      where=" (запуск $k: $grun)"
    else
      tag=$name
      runre=${RT_GO_RUN:-^TestRouter}
      where=""
    fi
    raw="$ART/go-$tag.raw"
    jsonl="$ART/go-$tag.jsonl"
    errlog="$ART/go-$tag.stderr"
    parsed="$ART/go-$tag.parsed"
    # Запуск идёт через небольшой скрипт: рядом с тестом работает опрос VmHWM (пиковая
    # память тестового бинарника), последняя строка попадает в go-<имя>.hwm отчёта.
    runner="$RT_REPORT/$ID/.go-run.sh"
    cat >"$runner" <<EOF
cd $RWD
rm -f $RWD/expect-version
XCP_RT_ARCH=$ARCH XCP_RT_CORE=$CORE XCP_RT_WORKDIR=$RWD $SESSION_ENV $LINKS_ENV $INPUTS_ENV $CONTRACT_ENV $ROUTING_CONTRACT_ENV $SETUP_ENV $SUB_ENV ./$name.test -test.v=test2json -test.run '$runre' -test.timeout 120m &
T=\$!
( while kill -0 \$T 2>/dev/null; do grep VmHWM /proc/\$T/status > $RWD/$tag.hwm 2>/dev/null; sleep 1; done ) &
W=\$!
wait \$T
rc=\$?
kill \$W 2>/dev/null
exit \$rc
EOF
    rc=0
    rt_scp "$ID" "$runner" "$RWD/run-$tag.sh" || rc=$?
    rm -f "$runner"
    if [ "$rc" = 3 ]; then
      LOST=1
      break
    fi
    rc=0
    rt_stage "$ID" "go-run-$tag" rt_ssh "$ID" "sh $RWD/run-$tag.sh" >"$raw" 2>"$errlog" || rc=$?
    if [ "$rc" = 3 ]; then
      LOST=1
      rt_result "$ID" FAIL "go:$name" "связь потеряна во время запуска тестов$where"
      break
    fi
    # stderr устройства: адреса и токены скрыты
    rt_redact <"$errlog" >"$errlog.red" && mv "$errlog.red" "$errlog"

    rt_ssh "$ID" "cat $RWD/$tag.hwm" >"$ART/go-$tag.hwm" 2>/dev/null || true
    go tool test2json -t -p "$pkg" <"$raw" >"$jsonl"
    # Счёт на запуск (todo go-suite-cumulative-bad-hides-binary-exit): PKG_BAD — FAIL и XPASS
    # этого запуска, PKG_QFAIL — упавшие тесты в карантине (в BAD и PKG_BAD не входят, D-17).
    found=0
    PKG_BAD=0
    PKG_QFAIL=0
    # строка провала запуска go:<пакет> этой группы (её нет в parsed): для suspect при смене версии
    grp_fail=""
    parse_jsonl "$pkg" <"$jsonl" >"$parsed"
    while IFS="$TAB" read -r st chk det || [ -n "$st" ]; do
      found=$((found + 1))
      rt_result "$ID" "$st" "$chk" "$det"
      case "$st" in
        FAIL | XPASS)
          BAD=$((BAD + 1))
          PKG_BAD=$((PKG_BAD + 1))
          ;;
        QFAIL) PKG_QFAIL=$((PKG_QFAIL + 1)) ;;
      esac
    done <"$parsed"
    if [ "$found" = 0 ]; then
      rt_result "$ID" FAIL "go:$name" "тесты не найдены или бинарник не запустился (код $rc)$where: $(tail -n 2 "$errlog" | tr '\n' ' ')"
      BAD=$((BAD + 1))
      grp_fail="go:$name"
    elif [ "$rc" != 0 ] && [ "$PKG_BAD" = 0 ] && [ "$PKG_QFAIL" = 0 ]; then
      # ненулевой код без упавших тестов этого запуска (panic в TestMain, os.Exit); упавший тест
      # в карантине объясняет код 1 и строки FAIL не даёт
      rt_result "$ID" FAIL "go:$name" "бинарник завершился с кодом $rc без упавших тестов$where"
      BAD=$((BAD + 1))
      grp_fail="go:$name"
    fi

    # Граница группы (D-13, RTR-15): версия стенда сверяется с ожидаемой; объявление теста
    # ($RWD/expect-version) читается и удаляется, переход принимается только при PASS объявившего.
    if [ "$SEQ" = 1 ]; then
      vbound="go:$pkg#$k"
    else
      vbound="go:$pkg"
    fi
    vrc=0
    rt_version_guard "$ID" "$vbound" "$RWD/expect-version" "$parsed" || vrc=$?
    rm -f "$raw"
    if [ "$vrc" = 3 ]; then
      LOST=1
      break
    elif [ "$vrc" = 4 ]; then
      # падения этой группы — на сменённой версии (класс stand), остальные группы не запускаются
      vstop="$RT_REPORT/$ID/version-stop.tsv"
      vcore=${CORE:--}
      awk -F'\t' -v c="$vcore" '$1 == "FAIL" || $1 == "XPASS" { printf "suspect\t%s\t%s\n", c, $2 }' "$parsed" >>"$vstop"
      [ -z "$grp_fail" ] || printf 'suspect\t%s\t%s\n' "$vcore" "$grp_fail" >>"$vstop"
      _ifs2=$IFS
      IFS='
'
      set -f
      for _rest in $(printf '%s' "$GROUPS" | sed -n "$((_gi + 1)),\$p"); do
        rpkg=${_rest%%"$TAB"*}
        if [ "$SEQ" = 1 ]; then
          rexpr="^(${_rest#*"$TAB"})\$"
        else
          rexpr=${RT_GO_RUN:-^TestRouter}
        fi
        printf 'notrun\t%s\tGo: %s: %s\n' "$vcore" "$rpkg" "$rexpr" >>"$vstop"
      done
      IFS=$_ifs2
      set +f
      VSTOP=1
      break
    elif [ "$vrc" != 0 ]; then
      rt_result "$ID" FAIL version-guard "сверка версии стенда не выполнена (код $vrc, граница $vbound)"
      BAD=$((BAD + 1))
    fi
  fi

  # последняя группа пакета прошла без обрыва связи — пакет в go-done.txt
  if ! printf '%s' "$GROUPS" | sed -n "$((_gi + 1)),\$p" | cut -f1 | grep -qxF -e "$pkg"; then
    printf '%s\n' "$pkg" >>"$ART/go-done.txt"
  fi
done
IFS=$_ifs
set +f

if [ "$LOST" = 1 ]; then
  rt_result "$ID" FAIL reachable "нет связи в ходе Go-тестов"
  exit 3
fi
# смена версии стенда: строки stop и FAIL version-guard записал rt_version_guard
[ "$VSTOP" = 0 ] || exit 4
[ "$BAD" = 0 ] || exit 1
exit 0
