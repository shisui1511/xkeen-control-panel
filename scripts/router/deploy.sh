#!/bin/sh
# scripts/router/deploy.sh — установка xcp на цели проверки.
#
#   scripts/router/deploy.sh                       сборка ветки на все цели из локального конфига
#   scripts/router/deploy.sh --target <id|all>     только эту цель
#   scripts/router/deploy.sh --release [<тег>]     артефакт GitHub Release вместо сборки
#                                                  (по умолчанию тег на HEAD), с проверкой sha256
#   scripts/router/deploy.sh --no-frontend         не пересобирать frontend/dist
#
# Цели ставятся параллельно, у каждой своя строка итога. Код выхода: 0 — все цели
# готовы, 3 — хоть одна недоступна по ssh, иначе 1. Если блокировка не взята
# родителем (RT_LOCK_HELD=1), скрипт берёт её сам.
#
# Панель на цели уже установлена через scripts/setup.sh (init-скрипт S99xcp).
# Init-скрипт при каждой установке пересоздаётся тем же setup.sh (--init-script:
# без загрузки и перезапуска) — из рабочей копии, а для --release из тега: стенд
# получает init-скрипт той же версии и тем же путём, что и установка у пользователя.
# Бинарник заливается во временный файл и подменяется через mv после остановки
# процесса — без «Text file busy».
#
# Статус: одиночная установка релиза (--release без родительского run.sh) на цели mipsle
# публикует статус stand/mipsle-install на коммите тега (release-status.sh): success — xcp
# запущен и «xcp -v» совпал, failure — нет; при сбое связи (код цели 3) и сбое подготовки
# стенда (код цели 2: нет init-скрипта, setup.sh --init-script, scp) статуса нет.
# Установка из run.sh статус установки не ставит: там статус смоука публикует run.sh.
#
# Уборка build/ (scripts/tidy-lib.sh): --release оставляет в кэше релизов 3 новейших тега и
# разворачиваемый, одиночный запуск ротирует отчёты; сборки ветки ротирует make router-<арх>.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/../tidy-lib.sh"

BIN_PATH=/opt/sbin/xcp
INIT_SCRIPT=/opt/etc/init.d/S99xcp

TARGET=all
FRONTEND=1
RELEASE=0
RELEASE_TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --target)
      [ $# -ge 2 ] || {
        echo "deploy: --target требует id или all" >&2
        exit 2
      }
      TARGET=$2
      shift 2
      ;;
    --no-frontend)
      FRONTEND=0
      shift
      ;;
    --release)
      RELEASE=1
      shift
      case "${1:-}" in
        '' | --*) ;;
        *)
          RELEASE_TAG=$1
          shift
          ;;
      esac
      ;;
    -h | --help)
      sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "deploy: неизвестный аргумент: $1" >&2
      exit 2
      ;;
  esac
done

cd "$(rt_repo_root)"

if [ "$TARGET" != all ]; then
  ROUTERS=$TARGET
  export ROUTERS
fi
rt_load_targets
rt_lock
# Одиночный запуск (отчёт не задан родителем): статус установки релиза публикует сам deploy.sh.
STANDALONE=0
[ -n "${RT_REPORT:-}" ] || STANDALONE=1
rt_report_use

# --- что ставим -------------------------------------------------------------

ARCHES=""
for t in $RT_TARGETS; do
  a=$(rt_get "$t" ARCH)
  case "$a" in
    arm64 | mipsle | mips) ;;
    *)
      echo "deploy: у цели $t неизвестная архитектура: $a" >&2
      exit 2
      ;;
  esac
  case " $ARCHES " in
    *" $a "*) ;;
    *) ARCHES="$ARCHES $a" ;;
  esac
done

if [ "$RELEASE" = 1 ]; then
  if [ -z "$RELEASE_TAG" ]; then
    RELEASE_TAG=$(git describe --exact-match --tags HEAD 2>/dev/null || true)
    if [ -z "$RELEASE_TAG" ]; then
      echo "deploy: --release без тега требует HEAD на теге" >&2
      exit 1
    fi
  fi
  VERSION=$RELEASE_TAG
  REL_DIR="$(rt_repo_root)/build/router/release/$RELEASE_TAG"
  mkdir -p "$REL_DIR"
  command -v gh >/dev/null 2>&1 || {
    echo "deploy: для --release нужен gh" >&2
    exit 1
  }
  ASSETS=$(gh release view "$RELEASE_TAG" --json assets -q '.assets[].name')
  for a in $ARCHES; do
    asset="xcp_${RELEASE_TAG}_${a}"
    printf '%s\n' "$ASSETS" | grep -qxF "$asset" || {
      echo "deploy: в релизе $RELEASE_TAG нет ассета $asset" >&2
      exit 1
    }
    printf '%s\n' "$ASSETS" | grep -qxF "$asset.sha256" || {
      echo "deploy: в релизе $RELEASE_TAG нет ассета $asset.sha256" >&2
      exit 1
    }
    gh release download "$RELEASE_TAG" -p "$asset" -p "$asset.sha256" -D "$REL_DIR" --clobber
    # .sha256 хранит имя файла без каталога: сверяем внутри каталога загрузки.
    if ! (cd "$REL_DIR" && sha256sum -c "$asset.sha256"); then
      echo "deploy: sha256 ассета $asset не совпал — установка отменена" >&2
      exit 1
    fi
  done
  # Разворачиваемый тег уже в кэше и защищён; сбой ротации установку не прерывает.
  ( tidy_rotate_releases "$(rt_repo_root)" "$RELEASE_TAG" ) || echo "deploy: предупреждение: ротация кэша релизов не удалась" >&2
  bin_for() {
    printf '%s\n' "$REL_DIR/xcp_${RELEASE_TAG}_$1"
  }
  SETUP_SRC="$REL_DIR/setup.sh"
  git show "$RELEASE_TAG:scripts/setup.sh" >"$SETUP_SRC" 2>/dev/null || : >"$SETUP_SRC"
else
  # version.sh не считает версию от пре-релизных тегов, поэтому номер пре-релиза на HEAD
  # берётся из тега vX.Y.Z-beta.N; XCP_VERSION подхватит make. Явная
  # XCP_VERSION (проход план-сессии под номером вышедшего релиза) важнее тега на HEAD.
  BETA_TAG=$(git tag --points-at HEAD | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$' | sort -V | tail -1 || true)
  if [ -z "${XCP_VERSION:-}" ] && [ -n "$BETA_TAG" ] && [ -z "$(git status --porcelain --untracked-files=no)" ]; then
    XCP_VERSION="$BETA_TAG"
    export XCP_VERSION
  fi
  VERSION=$(sh scripts/version.sh)
  echo "deploy: сборка ветки $VERSION"
  if [ "$FRONTEND" = 1 ]; then
    [ -d frontend/node_modules ] || rt_stage all frontend-ci npm --prefix frontend ci
    rt_stage all frontend-build npm --prefix frontend run build
  fi
  for a in $ARCHES; do
    # Старые теги собираются целью keenetic-<арх>, новые — router-<арх>.
    mk="router-$a"
    grep -q "^$mk:" Makefile || mk="keenetic-$a"
    rt_stage all "build-$a" make "$mk" VERSION="$VERSION"
    [ -f "build/xcp_${VERSION}_${a}" ] || {
      echo "deploy: нет build/xcp_${VERSION}_${a}" >&2
      exit 1
    }
  done
  bin_for() {
    printf '%s\n' "build/xcp_${VERSION}_$1"
  }
  SETUP_SRC="$(rt_repo_root)/scripts/setup.sh"
fi
# setup.sh старых тегов не умеет пересоздавать init-скрипт отдельно: его init-скрипт
# на цели остаётся прежним.
SETUP_INIT=0
grep -q -- '--init-script)' "$SETUP_SRC" && SETUP_INIT=1

# Ожидаемая версия для смоука.
printf '%s\n' "$VERSION" >"$RT_REPORT/version"

# --- установка на одну цель --------------------------------------------------

# Init-скрипт S99xcp из setup.sh этой сборки (setup.sh читается из stdin, как при
# установке через curl | sh).
init_script_refresh() {
  RT_SSH_STDIN=1 rt_ssh "$1" "sh -s -- --init-script" <"$SETUP_SRC" >/dev/null
}

# Код цели (пишется в deploy.rc): 0 — готово; 1 — продукт (xcp не запустился, старый не вышел,
# «xcp -v» не ответил или версия не совпала); 2 — стенд или подготовка (нет init-скрипта,
# setup.sh --init-script не отработал, бинарник не залит); 3 — нет связи.
deploy_one() {
  id=$1
  arch=$(rt_get "$id" ARCH)
  bin=$(bin_for "$arch")
  rc=0
  rt_ssh "$id" "[ -x $INIT_SCRIPT ]" || rc=$?
  case "$rc" in
    0) ;;
    3)
      echo "deploy[$id]: нет связи"
      return 3
      ;;
    *)
      echo "deploy[$id]: нет $INIT_SCRIPT — сначала установите панель через scripts/setup.sh"
      return 2
      ;;
  esac
  if [ "$SETUP_INIT" = 1 ]; then
    rc=0
    rt_stage "$id" init-script init_script_refresh "$id" || rc=$?
    if [ "$rc" = 3 ]; then
      echo "deploy[$id]: нет связи"
      return 3
    elif [ "$rc" != 0 ]; then
      echo "deploy[$id]: init-скрипт не пересоздан (setup.sh --init-script, код $rc)"
      return 2
    fi
  fi
  rc=0
  rt_stage "$id" upload rt_scp "$id" "$bin" "$BIN_PATH.new" || rc=$?
  if [ "$rc" = 3 ]; then
    echo "deploy[$id]: нет связи"
    return 3
  elif [ "$rc" != 0 ]; then
    echo "deploy[$id]: не удалось залить бинарник (код scp $rc)"
    return 2
  fi
  # Остановка с ожиданием полного выхода: пока старый процесс жив (даже после kill -9
  # он может дописывать на флешку), init-скрипт на запуск отвечает «already running»
  # и ничего не запускает — панель остаётся лежать (прогон 20261015-015433-24d29880).
  rc=0
  rt_stage "$id" install rt_ssh "$id" "
    set -e
    chmod +x $BIN_PATH.new
    pid=\$(pidof xcp || true)
    if [ -n \"\$pid\" ]; then
      kill \$pid 2>/dev/null || true
      i=0
      while pidof xcp >/dev/null; do
        i=\$((i + 1))
        if [ \$i -eq 20 ]; then kill -9 \$(pidof xcp) 2>/dev/null || true; fi
        if [ \$i -ge 30 ]; then echo 'старый xcp не вышел за 30 с' >&2; exit 4; fi
        sleep 1
      done
    fi
    mv -f $BIN_PATH.new $BIN_PATH
    # Запуск с ожиданием и одним повтором: на медленном устройстве первый запуск
    # после подмены 26 МБ бинарника иногда не поднимает процесс (mipsle).
    $INIT_SCRIPT start >/dev/null 2>&1 || true
    i=0
    while ! pidof xcp >/dev/null; do
      i=\$((i + 1))
      if [ \$i -eq 8 ]; then $INIT_SCRIPT start >/dev/null 2>&1 || true; fi
      if [ \$i -ge 30 ]; then echo 'xcp не запустился' >&2; exit 1; fi
      sleep 1
    done
    sleep 2
    pidof xcp >/dev/null || { echo 'xcp не запустился' >&2; exit 1; }
  " || rc=$?
  if [ "$rc" = 3 ]; then
    echo "deploy[$id]: нет связи"
    return 3
  elif [ "$rc" = 4 ]; then
    echo "deploy[$id]: старый xcp не вышел за 30 с, подмена не выполнена"
    return 1
  elif [ "$rc" != 0 ]; then
    echo "deploy[$id]: xcp не запустился после подмены"
    return 1
  fi
  rc=0
  installed=$(rt_ssh "$id" "$BIN_PATH -v") || rc=$?
  if [ "$rc" != 0 ]; then
    echo "deploy[$id]: не удалось получить версию xcp"
    return 1
  fi
  if [ "$installed" != "$VERSION" ]; then
    echo "deploy[$id]: на цели $installed, ожидалась $VERSION"
    return 1
  fi
  echo "deploy[$id]: ok $installed"
  return 0
}

PIDS=""
for t in $RT_TARGETS; do
  mkdir -p "$RT_REPORT/$t"
  (
    rc=0
    deploy_one "$t" || rc=$?
    printf '%s\n' "$rc" >"$RT_REPORT/$t/deploy.rc"
    exit "$rc"
  ) &
  PIDS="$PIDS $!"
done

OVERALL=0
for p in $PIDS; do
  rc=0
  wait "$p" || rc=$?
  if [ "$rc" = 3 ]; then
    OVERALL=3
  elif [ "$rc" != 0 ] && [ "$OVERALL" != 3 ]; then
    OVERALL=1
  fi
done

# Одиночная установка релиза на mipsle оставляет статус stand/mipsle-install на коммите тега.
# Сбой связи (код цели 3) и сбой подготовки стенда (код цели 2) статуса не дают: красная запись
# держит stable и должна означать только дефект продукта. Сбой публикации код выхода не меняет.
if [ "$STANDALONE" = 1 ] && [ "$RELEASE" = 1 ]; then
  for t in $RT_TARGETS; do
    [ "$(rt_get "$t" ARCH)" = mipsle ] || continue
    drc=1
    [ ! -f "$RT_REPORT/$t/deploy.rc" ] || drc=$(cat "$RT_REPORT/$t/deploy.rc")
    case "$drc" in
      0)
        st_state=success
        st_desc="установка mipsle: xcp запущен, версия совпала"
        ;;
      1)
        st_state=failure
        st_desc="установка mipsle: xcp не запустился или версия не совпала"
        ;;
      2)
        echo "deploy[$t]: предупреждение: статус установки не отправлен: сбой подготовки стенда" >&2
        continue
        ;;
      *)
        echo "deploy[$t]: статус установки не отправлен: сбой связи"
        continue
        ;;
    esac
    if ! sh "$SCRIPT_DIR/release-status.sh" "$RELEASE_TAG" stand/mipsle-install "$st_state" "$st_desc"; then
      echo "deploy[$t]: предупреждение: статус установки не отправлен (ошибка отправки)" >&2
    fi
  done
fi
# Одиночный запуск создал свой отчёт и держит блокировку rt_lock; при родительском run.sh
# отчёты ротирует он. Сбой ротации код выхода не меняет.
if [ "$STANDALONE" = 1 ]; then
  ( tidy_rotate_reports "$(rt_repo_root)" ) || echo "deploy: предупреждение: ротация отчётов не удалась" >&2
fi
exit "$OVERALL"
