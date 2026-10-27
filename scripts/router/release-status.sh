#!/bin/sh
# scripts/router/release-status.sh — публикация статуса стенда на коммите тега релиза.
#
#   release-status.sh [--dry-run] <тег> <контекст> <состояние> <описание>
#
#   тег        vX.Y.Z или пре-релиз vX.Y.Z-<суффикс>.<N> (суффикс — буквы, любой)
#   контекст   stand/<arch>-<набор>: stand/arm64-smoke, stand/arm64-full,
#              stand/mipsle-install
#   состояние  success, failure, error или pending
#   описание   непустое, не длиннее 140 символов; только фиксированные слова и
#              числа (набор, архитектура, итог, ядро, счёт падений)
#
# Коммит тега определяется через GitHub API, статус ставится на него. Статус
# виден всем в публичном репозитории и читается автовыпуском: ссылка на отчёт
# (target_url) не передаётся. Текст контекста и описания перед отправкой
# проверяется check-leaks.sh --file; найденная строка стенда — отказ, сама
# строка не печатается. Последний статус контекста побеждает: повторный прогон
# перекрывает прежний.
#
# С --dry-run запрос только печатается (после определения коммита и проверки
# утечек), ничего не отправляется.
#
# Код выхода: 0 — статус отправлен (или напечатан при --dry-run), 1 — отказ или
# ошибка GitHub (утечка в тексте, коммит тега не найден, ошибка gh), 2 — неверные
# аргументы.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

DRY_RUN=0
if [ "${1:-}" = "--dry-run" ]; then
  DRY_RUN=1
  shift
fi

if [ $# -ne 4 ]; then
  echo "release-status: использование: release-status.sh [--dry-run] <тег> <контекст> <состояние> <описание>" >&2
  exit 2
fi
TAG=$1
CTX=$2
STATE=$3
DESC=$4

# Значение должно быть одной строкой: grep -E иначе принял бы многострочный
# аргумент по одной подошедшей строке.
one_line() {
  case "$2" in
    *'
'*)
      echo "release-status: $1 должен быть одной строкой" >&2
      exit 2
      ;;
  esac
}
one_line тег "$TAG"
one_line контекст "$CTX"
one_line состояние "$STATE"
one_line описание "$DESC"

if ! printf '%s\n' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$'; then
  echo "release-status: неверный тег (ожидается vX.Y.Z или vX.Y.Z-<суффикс>.<N>)" >&2
  exit 2
fi
if ! printf '%s\n' "$CTX" | grep -Eq '^stand/[a-z0-9]+-[a-z]+$'; then
  echo "release-status: неверный контекст (ожидается stand/<arch>-<набор>)" >&2
  exit 2
fi
case "$STATE" in
  success | failure | error | pending) ;;
  *)
    echo "release-status: неверное состояние (success, failure, error или pending)" >&2
    exit 2
    ;;
esac
if [ -z "$DESC" ]; then
  echo "release-status: описание не может быть пустым" >&2
  exit 2
fi
# Символы, а не байты: непрерывные байты UTF-8 (0x80-0xBF) не считаются.
DESC_LEN=$(printf '%s' "$DESC" | LC_ALL=C tr -d '\200-\277' | wc -c | tr -d ' ')
if [ "$DESC_LEN" -gt 140 ]; then
  echo "release-status: описание длиннее 140 символов ($DESC_LEN)" >&2
  exit 2
fi

command -v gh >/dev/null 2>&1 || {
  echo "release-status: нужен gh" >&2
  exit 1
}

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
printf '%s\n%s\n' "$CTX" "$DESC" >"$TMP"
LEAK_RC=0
sh "$SCRIPT_DIR/check-leaks.sh" --file "$TMP" >/dev/null 2>&1 || LEAK_RC=$?
if [ "$LEAK_RC" -ne 0 ]; then
  echo "release-status: статус не отправлен: в тексте строки стенда" >&2
  exit 1
fi

SHA=$(gh api "repos/{owner}/{repo}/commits/$TAG" --jq .sha 2>/dev/null) || {
  echo "release-status: не удалось определить коммит тега $TAG" >&2
  exit 1
}
if ! printf '%s\n' "$SHA" | grep -Eq '^[0-9a-f]{40}$'; then
  echo "release-status: GitHub вернул неожиданный ответ для тега $TAG" >&2
  exit 1
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "release-status: DRY_RUN $SHA $CTX $STATE $DESC"
  exit 0
fi

if ! gh api -X POST "repos/{owner}/{repo}/statuses/$SHA" \
  -f state="$STATE" -f context="$CTX" -f description="$DESC" --silent; then
  echo "release-status: не удалось отправить статус $CTX на коммит тега $TAG" >&2
  exit 1
fi
SHORT=$(printf '%s' "$SHA" | cut -c1-8)
echo "release-status: $CTX $STATE на $SHORT"
