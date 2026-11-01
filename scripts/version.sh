#!/bin/sh
# scripts/version.sh — единый источник версии XCP (локально, в CI, в SW-кэше).
#
# Версия вычисляется только из git-истории, frontend/package.json не читается.
#
#   scripts/version.sh            версия сборки:
#                                   HEAD на теге vX.Y.Z или vX.Y.Z-beta.N, без правок → этот тег
#                                   иначе → v0.26.0-alpha.17+g578b6e8f[.dirty]
#   scripts/version.sh --next     база следующего релиза → v0.26.0
#
# Следующая версия: после последнего стабильного тега vX.Y.Z
#   feat…!: / BREAKING CHANGE → major (в 0.x — minor, пока API не стабилен)
#   feat:                      → minor
#   любые другие коммиты       → patch
# Если из HEAD достижим пре-релиз vB-beta.N новее последнего stable и B выше посчитанной версии,
# база — B: следующий выпуск будет пре-релизом этой базы (auto_release.sh plan), и сборка ветки
# не должна называть себя версией ниже уже вышедших пре-релизов. alpha.N — число коммитов после
# последнего stable.
#
# XCP_VERSION в окружении переопределяет версию сборки (ручной релиз из CI).

set -eu

MODE="${1:-build}"
case "$MODE" in
  build | --build) MODE=build ;;
  --next) MODE=next ;;
  -h | --help)
    sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
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

# Последний стабильный тег, достижимый из HEAD (пре-релизы -beta и сборки -alpha не считаются).
LAST_TAG=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' 2>/dev/null || true)

DIRTY=""
if [ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]; then
  DIRTY=".dirty"
fi

# В shallow-клоне без тегов (checkout CI по умолчанию) версию не угадываем:
# вызывающий должен сделать fetch-depth: 0 или задать XCP_VERSION.
if [ -z "$LAST_TAG" ] && [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = true ]; then
  echo "version.sh: shallow-клон без тегов, нужна полная история или XCP_VERSION" >&2
  exit 1
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

# max_pre — наибольший тег vX.Y.Z-beta.N со stdin числовым сравнением кортежа
# (major, minor, patch, N): sort -V есть не везде, а строкой beta.9 > beta.10.
max_pre() {
  awk '
    /^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$/ {
      s = substr($0, 2)
      sub(/-beta\./, ".", s)
      split(s, p, ".")
      k = sprintf("%09d%09d%09d%09d", p[1], p[2], p[3], p[4])
      if (k > best) { best = k; tag = $0 }
    }
    END { if (tag != "") print tag }
  '
}

# Пре-релиз на самом HEAD без правок — это и есть версия сборки
if [ "$MODE" = build ] && [ -z "$DIRTY" ]; then
  HEAD_PRE=$(git tag --points-at HEAD 2>/dev/null | max_pre)
  if [ -n "$HEAD_PRE" ]; then
    echo "$HEAD_PRE"
    exit 0
  fi
fi

# Новейший пре-релиз, достижимый из HEAD; его база — кандидат в базу версии
PRE_TAG=$(git tag -l 'v*-beta.*' --merged HEAD 2>/dev/null | max_pre)
PRE_BASE=""
if [ -n "$PRE_TAG" ]; then
  PRE_BASE="${PRE_TAG#v}"
  PRE_BASE="${PRE_BASE%-beta.*}"
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

# ver_gt A B — A строго выше B (X.Y.Z, числовое сравнение)
ver_gt() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    split(a, x, "."); split(b, y, ".")
    for (i = 1; i <= 3; i++) {
      if (x[i] + 0 > y[i] + 0) exit 0
      if (x[i] + 0 < y[i] + 0) exit 1
    }
    exit 1
  }'
}

if [ -n "$PRE_BASE" ] && ver_gt "$PRE_BASE" "$NEXT"; then
  NEXT="$PRE_BASE"
fi

case "$MODE" in
  next) echo "v$NEXT" ;;
  build)
    SHA=$(git rev-parse --short HEAD)
    echo "v$NEXT-alpha.$COUNT+g$SHA$DIRTY"
    ;;
esac
