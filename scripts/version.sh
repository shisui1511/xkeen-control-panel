#!/bin/sh
# scripts/version.sh — единый источник версии XCP (локально, в CI, в SW-кэше).
#
# Версия вычисляется только из git-истории, frontend/package.json не читается.
#
#   scripts/version.sh            версия сборки:
#                                   HEAD на релизном теге, без правок → v0.25.4
#                                   иначе → v0.26.0-dev.17+g578b6e8f[.dirty]
#   scripts/version.sh --next     следующий релиз по conventional commits → v0.26.0
#   scripts/version.sh --channel  имя rolling dev-релиза (тег и ассеты) → v0.26.0-dev,
#                                 на релизном теге — сам тег
#
# Следующая версия: после последнего стабильного тега vX.Y.Z
#   feat…!: / BREAKING CHANGE → major (в 0.x — minor, пока API не стабилен)
#   feat:                      → minor
#   любые другие коммиты       → patch
#
# XCP_VERSION в окружении переопределяет версию сборки (ручной релиз из CI).

set -eu

MODE="${1:-build}"
case "$MODE" in
  build | --build) MODE=build ;;
  --next) MODE=next ;;
  --channel) MODE=channel ;;
  -h | --help)
    sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
  *)
    echo "version.sh: неизвестный режим '$1'" >&2
    exit 2
    ;;
esac

if [ "$MODE" = build ] && [ -n "${XCP_VERSION:-}" ]; then
  case "$XCP_VERSION" in
    v*) echo "$XCP_VERSION" ;;
    *) echo "v$XCP_VERSION" ;;
  esac
  exit 0
fi

cd "$(dirname "$0")/.."

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "version.sh: нет git-репозитория, задайте XCP_VERSION" >&2
  exit 1
fi

# Последний стабильный тег, достижимый из HEAD (pre-release и -dev не считаются).
LAST_TAG=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' 2>/dev/null || true)

DIRTY=""
if [ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]; then
  DIRTY=".dirty"
fi

if [ -n "$LAST_TAG" ]; then
  RANGE="$LAST_TAG..HEAD"
  BASE="${LAST_TAG#v}"
else
  RANGE="HEAD"
  BASE="0.0.0"
fi

COUNT=$(git rev-list --count "$RANGE" 2>/dev/null || echo 0)

if [ -n "$LAST_TAG" ] && [ "$COUNT" = 0 ] && [ -z "$DIRTY" ] && [ "$MODE" != next ]; then
  echo "$LAST_TAG"
  exit 0
fi

MAJOR=$(echo "$BASE" | cut -d. -f1)
MINOR=$(echo "$BASE" | cut -d. -f2)
PATCH=$(echo "$BASE" | cut -d. -f3)

LOG=$(git log --format='%s%n%b' "$RANGE" 2>/dev/null || true)
if echo "$LOG" | grep -qE '^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE'; then
  BUMP=major
elif echo "$LOG" | grep -qE '^feat(\([^)]*\))?:'; then
  BUMP=minor
else
  BUMP=patch
fi
if [ "$BUMP" = major ] && [ "$MAJOR" = 0 ]; then
  BUMP=minor
fi

case "$BUMP" in
  major) NEXT="$((MAJOR + 1)).0.0" ;;
  minor) NEXT="$MAJOR.$((MINOR + 1)).0" ;;
  patch) NEXT="$MAJOR.$MINOR.$((PATCH + 1))" ;;
esac

case "$MODE" in
  next) echo "v$NEXT" ;;
  channel) echo "v$NEXT-dev" ;;
  build)
    SHA=$(git rev-parse --short HEAD)
    echo "v$NEXT-dev.$COUNT+g$SHA$DIRTY"
    ;;
esac
