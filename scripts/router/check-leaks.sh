#!/bin/sh
# scripts/router/check-leaks.sh — в git не должно быть ничего, что указывает на стенд.
#
# Из локального файла целей (scripts/router/targets.local.env основной копии)
# собирается список запрещённых строк: ssh-алиасы, хосты панелей, пароли, URL
# подписки и его хост, плюс XCP_LEAK_EXTRA (строки через перевод строки: адрес
# ПК, путь домашнего каталога). Они ищутся в отслеживаемых файлах и в ещё не
# отслеживаемых (без игнорируемых). Печатаются только «файл:строка» — без самой
# строки.
#
# Код выхода: 0 — утечек нет (или нет локального файла целей), 1 — найдены.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

cd "$(rt_repo_root)"

if ! TF=$(XCP_TARGETS_FILE="${XCP_TARGETS_FILE:-}" rt_targets_file 2>/dev/null); then
  echo "check-leaks: нет локального файла целей — проверять нечего" >&2
  exit 0
fi

TMP=$(mktemp -d)
chmod 700 "$TMP"
trap 'rm -rf "$TMP"' EXIT INT TERM
: >"$TMP/plain"
: >"$TMP/words"
: >"$TMP/hits"

# Добавляет строку в список (IPv4 — в список поиска по границам слов, чтобы
# 192.0.2.1 не находил 192.0.2.10). Слишком короткое пропускается.
add() {
  [ -n "$1" ] || return 0
  if [ "${#1}" -lt 4 ]; then
    SKIPPED=$((SKIPPED + 1))
    return 0
  fi
  case "$1" in
    *[!0-9.]* | '') printf '%s\n' "$1" >>"$TMP/plain" ;;
    *) printf '%s\n' "$1" >>"$TMP/words" ;;
  esac
}

host_of() {
  printf '%s' "$1" | sed -E 's#^[A-Za-z][A-Za-z0-9+.-]*://##; s#^[^@/]*@##; s#[/:?#].*$##'
}

SKIPPED=0
# Файл целей читается в подоболочке: его переменные не попадают в текущую среду.
(
  # shellcheck disable=SC1090
  . "$TF"
  for id in ${XCP_TARGETS:-}; do
    case "$id" in '' | *[!a-z0-9]*) continue ;; esac
    eval "ssh_=\${XCP_T_${id}_SSH:-} url_=\${XCP_T_${id}_URL:-} pw_=\${XCP_T_${id}_PASSWORD:-}"
    printf '%s\n' "$ssh_" "$(host_of "$url_")" "$pw_"
  done
  printf '%s\n' "${XCP_SUBSCRIPTION_URL:-}" "$(host_of "${XCP_SUBSCRIPTION_URL:-}")"
  printf '%s\n' "${XCP_LEAK_EXTRA:-}"
) >"$TMP/all"

while IFS= read -r line; do
  add "$line"
done <"$TMP/all"
rm -f "$TMP/all"

TOTAL=$(($(wc -l <"$TMP/plain") + $(wc -l <"$TMP/words")))
if [ "$TOTAL" -eq 0 ]; then
  echo "check-leaks: в локальном файле целей нет строк для проверки" >&2
  exit 0
fi

# Отслеживаемые файлы.
[ ! -s "$TMP/plain" ] || git grep -nIF -f "$TMP/plain" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
[ ! -s "$TMP/words" ] || git grep -nIFw -f "$TMP/words" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true

# Ещё не отслеживаемые файлы (игнорируемые не берём).
git ls-files --others --exclude-standard -z >"$TMP/untracked"
if [ -s "$TMP/untracked" ]; then
  [ ! -s "$TMP/plain" ] || xargs -0 grep -HnIF -f "$TMP/plain" -- <"$TMP/untracked" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
  [ ! -s "$TMP/words" ] || xargs -0 grep -HnIFw -f "$TMP/words" -- <"$TMP/untracked" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
fi

sort -u "$TMP/hits" >"$TMP/real"

if [ "$SKIPPED" -gt 0 ]; then
  echo "check-leaks: пропущено слишком коротких строк (меньше 4 символов): $SKIPPED"
fi
if [ -s "$TMP/real" ]; then
  echo "check-leaks: в git найдены строки из локального файла целей (только файл:строка):" >&2
  sed 's/^/  /' "$TMP/real" >&2
  exit 1
fi
echo "check-leaks: утечек нет"
