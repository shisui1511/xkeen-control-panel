#!/usr/bin/env bash
# .github/scripts/auto_release_test.sh — тесты решений auto_release.sh (plan, promote)
# на временном git-репозитории. Запуск: bash .github/scripts/auto_release_test.sh

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
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

REPO=$(mktemp -d)
trap 'rm -rf "$REPO"' EXIT
mkdir -p "$REPO/scripts"
cp "$HERE/../../scripts/version.sh" "$REPO/scripts/version.sh"
git -C "$REPO" init -q
git -C "$REPO" config user.email t@t
git -C "$REPO" config user.name t
git -C "$REPO" config commit.gpgsign false
git -C "$REPO" config tag.gpgsign false
echo "scripts/" >"$REPO/.gitignore"

T0=1800000000
HOUR=3600

commit() {
  echo "$1" >>"$REPO/file.txt"
  git -C "$REPO" add file.txt .gitignore
  git -C "$REPO" commit -q -m "$1"
}

# Аннотированный тег с заданным временем (как ставит auto-release)
atag() { GIT_COMMITTER_DATE="@$2 +0000" git -C "$REPO" tag -a "$1" -m "$1" "${3:-HEAD}"; }

ar() { (cd "$REPO" && NOW="${NOW:-$T0}" SOAK_HOURS=24 bash "$HERE/auto_release.sh" "$@"); }

echo "auto_release.sh"

commit "chore: init"
check "без тегов и без feat/fix — нечего выпускать" "$(ar plan)" "none нет feat/fix/perf/refactor после начала истории"
check "без RC — нечего продвигать" "$(ar promote)" "none нет RC новее "

git -C "$REPO" tag v0.1.0
commit "docs: readme"
commit "chore(deps): bump"
check "только chore/docs — нечего выпускать" "$(ar plan)" "none нет feat/fix/perf/refactor после v0.1.0"

commit "fix(api): падение"
check "fix — сразу stable patch" "$(ar plan)" "stable v0.1.1"
commit "perf: быстрее"
check "fix+perf — всё ещё patch" "$(ar plan)" "stable v0.1.1"

git -C "$REPO" tag v0.1.1
commit "feat(ui): новая страница"
check "feat — RC минорной версии" "$(ar plan)" "rc v0.2.0-rc.1"

atag v0.2.0-rc.1 "$T0"
check "RC уже стоит на HEAD — тот же ответ" "$(ar plan)" "rc v0.2.0-rc.1"
commit "chore: tooling"
check "после RC только chore — новый RC не нужен" "$(ar plan)" "rc v0.2.0-rc.1"
commit "fix(ui): поправка к RC"
check "fix после RC — следующий RC той же версии" "$(ar plan)" "rc v0.2.0-rc.2"

check "fix после RC прерывает выдержку, пока не выйдет новый RC" \
  "$(NOW=$((T0 + 30 * HOUR)) ar promote)" "none после v0.2.0-rc.1 есть новые изменения, ждём следующий RC"
atag v0.2.0-rc.2 $((T0 + 10 * HOUR))
check "свежий RC ещё выдерживается" "$(NOW=$((T0 + 20 * HOUR)) ar promote)" "none v0.2.0-rc.2 в beta 10 ч из 24"
check "RC провисел 24 ч — stable из его коммита" "$(NOW=$((T0 + 34 * HOUR)) ar promote)" "stable v0.2.0 v0.2.0-rc.2"
commit "chore: после RC"
check "chore после RC выдержку не прерывает" "$(NOW=$((T0 + 34 * HOUR)) ar promote)" "stable v0.2.0 v0.2.0-rc.2"

git -C "$REPO" tag v0.2.0 v0.2.0-rc.2^{}
check "после stable RC этой версии не продвигаются" "$(NOW=$((T0 + 99 * HOUR)) ar promote)" "none нет RC новее v0.2.0"
check "после stable без новых feat/fix — нечего выпускать" "$(ar plan)" "none нет feat/fix/perf/refactor после v0.2.0"

commit "fix: после релиза"
check "fix после stable — patch" "$(ar plan)" "stable v0.2.1"
commit "feat(core)!: ломающее изменение"
check "feat! в 0.x — минорный RC" "$(ar plan)" "rc v0.3.0-rc.1"

atag v0.3.0-rc.9 "$T0"
atag v0.3.0-rc.10 "$T0"
check "promote берёт rc.10, а не rc.9" "$(NOW=$((T0 + 25 * HOUR)) ar promote)" "stable v0.3.0 v0.3.0-rc.10"
commit "fix: ещё"
check "номер RC после rc.10 сравнивается численно" "$(ar plan)" "rc v0.3.0-rc.11"

echo
echo "PASS: $PASS  FAIL: $FAIL"
[ "$FAIL" = 0 ]
