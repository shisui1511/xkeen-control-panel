#!/bin/sh
# scripts/version_test.sh — тесты scripts/version.sh на временных git-репозиториях.
# Запуск: sh scripts/version_test.sh

set -eu

SCRIPT="$(cd "$(dirname "$0")" && pwd)/version.sh"
PASS=0
FAIL=0

check() {
  if [ "$2" = "$3" ]; then
    PASS=$((PASS + 1))
    printf "  PASS  %s\n" "$1"
  else
    FAIL=$((FAIL + 1))
    printf "  FAIL  %s: ожидалось '%s', получено '%s'\n" "$1" "$3" "$2"
  fi
}

# Песочница: копия скрипта в scripts/ отдельного репозитория.
new_repo() {
  REPO=$(mktemp -d)
  mkdir -p "$REPO/scripts"
  cp "$SCRIPT" "$REPO/scripts/version.sh"
  git -C "$REPO" init -q
  git -C "$REPO" config user.email t@t
  git -C "$REPO" config user.name t
  git -C "$REPO" config commit.gpgsign false
  git -C "$REPO" config tag.gpgsign false
  echo "scripts/" >"$REPO/.gitignore"
}

commit() {
  echo "$1" >>"$REPO/file.txt"
  git -C "$REPO" add file.txt .gitignore
  git -C "$REPO" commit -q -m "$1"
}

v() { (cd "$REPO" && env -u XCP_VERSION sh scripts/version.sh "$@"); }
sha() { git -C "$REPO" rev-parse --short HEAD; }

echo "version.sh"

new_repo
commit "chore: init"
check "без тегов — от 0.0.0" "$(v)" "v0.0.1-dev.1+g$(sha)"

git -C "$REPO" tag v0.25.4
check "HEAD на релизном теге" "$(v)" "v0.25.4"
check "канал на релизном теге" "$(v --channel)" "v0.25.4"

commit "fix(backend): поправка"
check "fix после тега → patch" "$(v)" "v0.25.5-dev.1+g$(sha)"
check "канал для fix" "$(v --channel)" "v0.25.5-dev"

commit "feat(frontend): фича"
check "feat после тега → minor" "$(v)" "v0.26.0-dev.2+g$(sha)"
check "следующий релиз" "$(v --next)" "v0.26.0"

git -C "$REPO" tag v0.26.0-dev
commit "docs: заметка"
check "dev-теги не считаются базой" "$(v)" "v0.26.0-dev.3+g$(sha)"

commit "feat(api)!: несовместимое изменение"
check "breaking в 0.x → minor" "$(v --next)" "v0.26.0"

git -C "$REPO" tag v1.2.3
commit "refactor: что-то

BREAKING CHANGE: убран старый эндпоинт"
check "breaking в 1.x → major" "$(v --next)" "v2.0.0"

echo change >>"$REPO/file.txt"
check "незакоммиченные правки → .dirty" "$(v)" "v2.0.0-dev.1+g$(sha).dirty"

check "XCP_VERSION переопределяет" "$(cd "$REPO" && XCP_VERSION=0.30.0 sh scripts/version.sh)" "v0.30.0"

SHALLOW=$(mktemp -d)
git clone -q --depth 1 --no-tags "file://$REPO" "$SHALLOW/r" 2>/dev/null
mkdir -p "$SHALLOW/r/scripts" && cp "$SCRIPT" "$SHALLOW/r/scripts/version.sh"
if (cd "$SHALLOW/r" && env -u XCP_VERSION sh scripts/version.sh >/dev/null 2>&1); then rc=0; else rc=1; fi
check "shallow-клон без тегов — ошибка, а не 0.0.1" "$rc" "1"

printf "\nИтого: %d пройдено, %d провалено\n" "$PASS" "$FAIL"
[ "$FAIL" = 0 ]
