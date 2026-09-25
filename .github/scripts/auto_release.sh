#!/usr/bin/env bash
# Автоматический выпуск release candidate и стабильных релизов (workflow auto-release.yml).
#
#   auto_release.sh plan     что выпустить для HEAD (последний коммит main):
#                              "stable vX.Y.Z"      — с прошлого релиза только fix/perf/refactor/revert
#                              "rc vX.Y.Z-rc.N"     — есть feat или ломающие изменения
#                              "none <причина>"     — выпускать нечего
#   auto_release.sh promote  какой RC пора сделать стабильным:
#                              "stable vX.Y.Z vX.Y.Z-rc.N" — RC провисел в beta SOAK_HOURS, и после
#                                                          него в main нет новых feat/fix
#                              "none <причина>"
#   auto_release.sh run      полный цикл в CI: promote, затем plan для зелёного main;
#                            ставит тег и запускает build.yml через workflow_dispatch
#                            (тег, запушенный GITHUB_TOKEN, сам сборку не запускает).
#
# plan и promote решают только по git (теги и сообщения коммитов) и не ходят в GitHub,
# их проверяет .github/scripts/auto_release_test.sh. Ответ идемпотентен: если нужный тег
# уже есть, он возвращается снова, а run сверяет, опубликован ли по нему релиз.
#
# Окружение:
#   SOAK_HOURS  сколько часов RC должен провисеть без замены (по умолчанию 24)
#   NOW         текущее время, unix (для тестов)
#   DRY_RUN=1   run только печатает решения
#   RUN_SHA     коммит, чей CI завершился (workflow_run); пусто — берётся main

set -euo pipefail

SOAK_HOURS="${SOAK_HOURS:-24}"
NOW="${NOW:-$(date +%s)}"

# Коммиты, ради которых выходит релиз; chore/docs/test/ci бинарник не меняют
WORTHY='^(feat|fix|perf|refactor|revert)(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE'
STABLE_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'

ROOT=$(git rev-parse --show-toplevel)

last_stable() {
  git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' "${1:-HEAD}" 2>/dev/null || true
}

release_worthy() {
  local log
  log=$(git log --format='%s%n%b' "$1" 2>/dev/null || true)
  grep -qE "$WORTHY" <<<"$log"
}

tag_exists() {
  git rev-parse -q --verify "refs/tags/$1" >/dev/null
}

# Время создания тега: для аннотированного — дата тега, для лёгкого — дата коммита
tag_time() {
  git for-each-ref --format='%(if)%(taggerdate)%(then)%(taggerdate:unix)%(else)%(committerdate:unix)%(end)' "refs/tags/$1"
}

# Новейший RC, достижимый из HEAD: версии $1, а без аргумента — любой версии новее
# последнего стабильного
latest_rc() {
  local base="${1:-}" last tags tag found=""
  tags=$(git tag -l "${base:-v}*-rc.*" --merged HEAD | grep -E -- '-rc\.[0-9]+$' | sort -V || true)
  last=$(last_stable)
  for tag in $tags; do
    if [ -z "$base" ] && [ -n "$last" ] &&
      [ "$(printf '%s\n%s\n' "${tag%-rc.*}" "$last" | sort -V | tail -1)" = "$last" ]; then
      continue
    fi
    found="$tag"
  done
  echo "$found"
}

plan() {
  local last range next rc n
  last=$(last_stable)
  range="${last:+$last..}HEAD"
  if ! release_worthy "$range"; then
    echo "none нет feat/fix/perf/refactor после ${last:-начала истории}"
    return
  fi

  next=$(sh "$ROOT/scripts/version.sh" --next)
  case "$next" in
    *.0)
      rc=$(latest_rc "$next")
      if [ -n "$rc" ] && ! release_worthy "$rc..HEAD"; then
        echo "rc $rc"
        return
      fi
      n=0
      [ -z "$rc" ] || n="${rc##*-rc.}"
      echo "rc $next-rc.$((n + 1))"
      ;;
    *) echo "stable $next" ;;
  esac
}

promote() {
  local rc base age need
  rc=$(latest_rc)
  if [ -z "$rc" ]; then
    echo "none нет RC новее $(last_stable)"
    return
  fi
  base="${rc%-rc.*}"
  if tag_exists "$base"; then
    echo "none $base уже выпущен"
    return
  fi
  if release_worthy "$rc..HEAD"; then
    echo "none после $rc есть новые изменения, ждём следующий RC"
    return
  fi
  age=$((NOW - $(tag_time "$rc")))
  need=$((SOAK_HOURS * 3600))
  if [ "$age" -lt "$need" ]; then
    echo "none $rc в beta $((age / 3600)) ч из $SOAK_HOURS"
    return
  fi
  echo "stable $base $rc"
}

# ── Всё ниже нужно только в CI (gh, push, dispatch) ──

log() { echo "auto-release: $*"; }

# Все push-прогоны коммита, кроме самого auto-release, завершены успешно.
# Возвращает 0 — зелёный, 1 — есть красный, 2 — ещё идут (их завершение снова запустит нас).
ci_state() {
  local sha="$1" runs
  runs=$(gh run list --commit "$sha" --event push --limit 50 \
    --json workflowName,status,conclusion \
    --jq '.[] | select(.workflowName != "Auto Release") | "\(.status) \(.conclusion) \(.workflowName)"')
  if [ -z "$runs" ]; then
    return 2
  fi
  if grep -qv '^completed ' <<<"$runs"; then
    return 2
  fi
  if grep -vqE '^completed (success|skipped|neutral) ' <<<"$runs"; then
    grep -vE '^completed (success|skipped|neutral) ' <<<"$runs" | sed 's/^/  /'
    return 1
  fi
  return 0
}

# Ставит тег (если его нет) и запускает сборку. Идемпотентно: при опубликованном релизе
# ничего не делает, при идущей сборке ждёт, после двух упавших сборок сдаётся.
publish() {
  local tag="$1" sha="$2" active failed
  if gh release view "$tag" >/dev/null 2>&1; then
    log "$tag уже опубликован"
    return
  fi
  if [ "${DRY_RUN:-0}" = 1 ]; then
    log "DRY_RUN: выпустил бы $tag на ${sha:0:8}"
    return
  fi
  if ! tag_exists "$tag"; then
    git tag -a "$tag" -m "$tag" "$sha"
    git push origin "refs/tags/$tag"
    log "тег $tag поставлен на ${sha:0:8}"
  fi
  active=$(gh run list --workflow build.yml --branch "$tag" --json status \
    --jq '[.[] | select(.status != "completed")] | length')
  failed=$(gh run list --workflow build.yml --branch "$tag" --json conclusion \
    --jq '[.[] | select(.conclusion == "failure")] | length')
  if [ "$active" -gt 0 ]; then
    log "сборка $tag уже идёт"
  elif [ "$failed" -ge 2 ]; then
    log "сборка $tag упала $failed раза, нужен ручной разбор"
    exit 1
  else
    gh workflow run build.yml --ref "$tag" -f version="${tag#v}"
    log "запущена сборка $tag"
  fi
}

run() {
  if [ "${GITHUB_ACTIONS:-}" = true ]; then
    git config user.name "github-actions"
    git config user.email "github-actions@github.com"
  fi

  local decision kind tag rc main_sha state
  decision=$(promote)
  log "promote: $decision"
  read -r kind tag rc <<<"$decision"
  if [ "$kind" = stable ]; then
    publish "$tag" "$(git rev-list -n1 "$rc")"
  fi

  main_sha=$(git rev-parse HEAD)
  if [ -n "${RUN_SHA:-}" ] && [ "$RUN_SHA" != "$main_sha" ]; then
    log "CI завершился для ${RUN_SHA:0:8}, но main уже на ${main_sha:0:8} — решит прогон нового коммита"
    return
  fi
  state=0
  ci_state "$main_sha" || state=$?
  case "$state" in
    1)
      log "CI для ${main_sha:0:8} красный, релиз не выпускается"
      return
      ;;
    2)
      log "CI для ${main_sha:0:8} ещё идёт"
      return
      ;;
  esac

  decision=$(plan)
  log "plan: $decision"
  read -r kind tag _ <<<"$decision"
  case "$kind" in
    stable | rc) publish "$tag" "$(git rev-list -n1 "$tag" 2>/dev/null || echo "$main_sha")" ;;
  esac
}

case "${1:-}" in
  plan) plan ;;
  promote) promote ;;
  run) run ;;
  *)
    sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
