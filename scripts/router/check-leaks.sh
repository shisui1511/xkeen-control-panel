#!/bin/sh
# scripts/router/check-leaks.sh — в git не должно быть ничего, что указывает на стенд.
#
# Из локального файла целей (scripts/router/targets.local.env основной копии)
# собирается список запрещённых строк: ssh-алиасы, хосты панелей, пароли, URL
# подписки и его хост, плюс XCP_LEAK_EXTRA (строки через перевод строки: адрес
# ПК, путь домашнего каталога, альтернативные ssh-алиасы стенда). Для каждого
# ssh-алиаса добавляются ещё hostname и proxyjump из «ssh -G» (с hostname
# промежуточных узлов): адрес, под которым алиас раскрывается в ~/.ssh/config,
# тоже не должен попасть в git. Цели берутся из имён всех переменных
# XCP_T_<id>_* файла, а не из выбора целей прогона (XCP_TARGETS и ROUTERS на
# охват не влияют); значения берутся по полному имени переменной, поэтому форма
# идентификатора на охват не влияет. Идентификатор вне [a-z0-9] — ошибка
# (код 2, имя переменной в stderr): rt_load_targets такую цель не принимает, а
# молча пропустить её здесь значило бы не проверять её строки. Охват виден по
# строке «check-leaks: цели: <id> <id> …» (только идентификаторы). Печатаются
# только «файл:строка» — без самой строки.
#
# Режимы:
#   check-leaks.sh           отслеживаемые файлы рабочего дерева и ещё не
#                            отслеживаемые (без игнорируемых)
#   check-leaks.sh --staged  только содержимое индекса (для pre-commit); нет
#                            локального файла целей — молча код 0
#   check-leaks.sh --file <путь>
#                            один файл (для commit-msg: сообщение коммита); нет
#                            локального файла целей — молча код 0
#
# Код выхода: 0 — утечек нет (или нет локального файла целей), 1 — найдены,
# 2 — неизвестный аргумент или идентификатор цели вне [a-z0-9], 130 — прервано
# сигналом INT, TERM или HUP.

set -eu

STAGED=0
ONE_FILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --staged)
      STAGED=1
      shift
      ;;
    --file)
      if [ $# -lt 2 ] || [ -z "$2" ]; then
        echo "check-leaks: --file требует путь" >&2
        exit 2
      fi
      ONE_FILE=$2
      shift 2
      ;;
    *)
      echo "check-leaks: неизвестный аргумент $1" >&2
      exit 2
      ;;
  esac
done
if [ "$STAGED" -eq 1 ] && [ -n "$ONE_FILE" ]; then
  echo "check-leaks: --staged и --file вместе не используются" >&2
  exit 2
fi

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

cd "$(rt_repo_root)"

if ! TF=$(XCP_TARGETS_FILE="${XCP_TARGETS_FILE:-}" rt_targets_file 2>/dev/null); then
  [ "$STAGED" -eq 1 ] || [ -n "$ONE_FILE" ] || echo "check-leaks: нет локального файла целей — проверять нечего" >&2
  exit 0
fi

TMP=$(mktemp -d)
chmod 700 "$TMP"
trap 'rm -rf "$TMP"' EXIT
trap 'exit 130' INT TERM HUP
: >"$TMP/plain"
: >"$TMP/words"
: >"$TMP/hits"
: >"$TMP/ids"
: >"$TMP/badnames"

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

# hostname и proxyjump алиаса из конфигурации ssh (ssh -G ничего не подключает), затем
# hostname промежуточных узлов. Нет ssh или алиас не разбирается — ничего не добавляется.
ssh_hosts() {
  command -v ssh >/dev/null 2>&1 || return 0
  [ -n "$1" ] || return 0
  _cfg=$(ssh -G "$1" 2>/dev/null) || return 0
  printf '%s\n' "$_cfg" | awk '$1 == "hostname" { print $2 }'
  _jumps=$(printf '%s\n' "$_cfg" | awk '$1 == "proxyjump" { print $2 }' | tr ',' '\n')
  for _j in $_jumps; do
    [ "$_j" != none ] || continue
    _j=${_j#*@}
    _j=${_j%%:*}
    printf '%s\n' "$_j"
    ssh -G "$_j" 2>/dev/null | awk '$1 == "hostname" { print $2 }' || true
  done
}

SKIPPED=0
# Файл целей читается в подоболочке: его переменные не попадают в текущую среду.
(
  # shellcheck disable=SC1090
  . "$TF"
  # Цели — по именам заданных переменных XCP_T_<id>_<КЛЮЧ>: выбор целей прогона
  # (XCP_TARGETS, ROUTERS) не читается. Вывод set идёт только в этот конвейер и
  # нигде не сохраняется; в $TMP/ids и $TMP/badnames попадают одни имена.
  # Значения берутся по полному имени, любой ключ, кроме ARCH (архитектура —
  # нейтральное слово, в git его много); форма идентификатора на охват не влияет.
  set | sed -n 's/^\(XCP_T_[A-Za-z0-9_]*\)=.*/\1/p' | sort -u >"$TMP/names"
  while IFS= read -r name; do
    case "$name" in XCP_T_*_ARCH) continue ;; esac
    rest=${name#XCP_T_}
    id=${rest%_*}
    key=${rest##*_}
    # без «_<КЛЮЧ>» после идентификатора разбирать нечего: id совпал бы с ключом
    if [ "$id" = "$rest" ]; then
      printf '%s\n' "$name" >>"$TMP/badnames"
      continue
    fi
    case "$id" in '' | *[!a-z0-9]*) printf '%s\n' "$name" >>"$TMP/badnames" ;; *) printf '%s\n' "$id" >>"$TMP/ids" ;; esac
    eval "val=\${$name:-}"
    case "$key" in
      URL)
        printf '%s\n' "$(host_of "$val")"
        ;;
      SSH)
        printf '%s\n' "$val"
        ssh_hosts "$val"
        ;;
      *) printf '%s\n' "$val" ;;
    esac
  done <"$TMP/names"
  printf '%s\n' "${XCP_SUBSCRIPTION_URL:-}" "$(host_of "${XCP_SUBSCRIPTION_URL:-}")"
  printf '%s\n' "${XCP_LEAK_EXTRA:-}"
) >"$TMP/all"

while IFS= read -r line; do
  add "$line"
done <"$TMP/all"
rm -f "$TMP/all"

if [ -s "$TMP/badnames" ]; then
  echo "check-leaks: идентификатор цели вне [a-z0-9] — строки такой цели не проверить, переименуйте (только имена):" >&2
  sort -u "$TMP/badnames" | sed 's/^/  /' >&2
  exit 2
fi

IDS=$(sort -u "$TMP/ids" | tr '\n' ' ' | sed 's/ $//')
echo "check-leaks: цели: ${IDS:-нет}"

TOTAL=$(($(wc -l <"$TMP/plain") + $(wc -l <"$TMP/words")))
if [ "$TOTAL" -eq 0 ]; then
  echo "check-leaks: в локальном файле целей нет строк для проверки" >&2
  exit 0
fi

# Один файл (сообщение коммита): те же списки, без git grep.
if [ -n "$ONE_FILE" ]; then
  if [ ! -f "$ONE_FILE" ]; then
    echo "check-leaks: нет файла $ONE_FILE" >&2
    exit 2
  fi
  [ ! -s "$TMP/plain" ] || grep -HnIF -f "$TMP/plain" -- "$ONE_FILE" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
  [ ! -s "$TMP/words" ] || grep -HnIFw -f "$TMP/words" -- "$ONE_FILE" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
  sort -u "$TMP/hits" >"$TMP/real"
  if [ -s "$TMP/real" ]; then
    echo "check-leaks: в файле найдены строки из локального файла целей (только файл:строка):" >&2
    sed 's/^/  /' "$TMP/real" >&2
    exit 1
  fi
  echo "check-leaks: утечек нет"
  exit 0
fi

# Отслеживаемые файлы (в режиме --staged — содержимое индекса).
CACHED=''
[ "$STAGED" -eq 0 ] || CACHED='--cached'
# shellcheck disable=SC2086
[ ! -s "$TMP/plain" ] || git grep $CACHED -nIF -f "$TMP/plain" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true
# shellcheck disable=SC2086
[ ! -s "$TMP/words" ] || git grep $CACHED -nIFw -f "$TMP/words" 2>/dev/null | cut -d: -f1,2 >>"$TMP/hits" || true

# Ещё не отслеживаемые файлы (игнорируемые не берём); в индекс они не входят.
: >"$TMP/untracked"
[ "$STAGED" -eq 1 ] || git ls-files --others --exclude-standard -z >"$TMP/untracked"
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
