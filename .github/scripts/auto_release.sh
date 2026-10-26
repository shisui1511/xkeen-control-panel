#!/usr/bin/env bash
# Автоматический выпуск release candidate и стабильных релизов (workflow auto-release.yml).
#
#   auto_release.sh plan     что выпустить для HEAD (последний коммит main):
#                              "stable vX.Y.Z"      — с прошлого релиза только fix/perf/refactor/revert
#                              "rc vX.Y.Z-rc.N"     — есть feat или ломающие изменения
#                              "none <причина>"     — выпускать нечего
#   auto_release.sh promote  какой пре-релиз пора сделать стабильным:
#                              "stable vX.Y.Z vX.Y.Z-rc.N" / "none <причина>";
#                            вердикты кандидатов печатаются в stderr
#   auto_release.sh heal     свежие (7 дней) теги релизов без опубликованного релиза:
#                              "delete vX.Y.Z-rc.N" — RC вытеснен более новым тегом
#                              "publish vX.Y.Z[-rc.N]" — актуальный тег, сборку повторить
#   auto_release.sh report   только чтение GitHub и git: вердикты по каждому пре-релизу новее
#                            последнего stable, итог promote и plan, markdown-сводка в stdout;
#                            ничего не меняет (можно запускать на ПК)
#   auto_release.sh run      полный цикл в CI: heal, promote, затем plan для зелёного main;
#                            ставит тег и запускает build.yml через workflow_dispatch
#                            (тег, запушенный GITHUB_TOKEN, сам сборку не запускает).
#
# Окружение:
#   SOAK_HOURS  сколько часов пре-релиз X.Y.0 должен провисеть после публикации (по умолчанию 24)
#   PRE_KIND    суффикс пре-релиза (по умолчанию rc): теги vX.Y.Z-<PRE_KIND>.N
#   PROMOTE_BASE версия vX.Y.Z из ручного запуска: снимает выдержку этой базы
#   RELEASE_STOP стоп выпуска: непустое значение, кроме 0 и false (переменная репозитория);
#               на ПК, если переменная окружения не задана, читается из репозитория
#   NOW         текущее время, unix
#   DRY_RUN=1   run только печатает решения
#   RUN_SHA     коммит, чей CI завершился (workflow_run); пусто — берётся main

set -euo pipefail

SOAK_HOURS="${SOAK_HOURS:-24}"
[[ "$SOAK_HOURS" =~ ^[0-9]+$ ]] || SOAK_HOURS=24
NOW="${NOW:-$(date +%s)}"
PRE_KIND="${PRE_KIND:-rc}"
[[ "$PRE_KIND" =~ ^[a-z]+$ ]] || {
  echo "auto-release: PRE_KIND должен состоять из строчных латинских букв" >&2
  exit 2
}

# Коммиты, ради которых выходит релиз; chore/docs/test/ci бинарник не меняют
WORTHY='^(feat|fix|perf|refactor|revert)(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE'
STABLE_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'
PRE_RE="^v[0-9]+\\.[0-9]+\\.[0-9]+-${PRE_KIND}\\.[0-9]+\$"
HEAL_DAYS=7
STOP_LABEL=release-stop
ARM_SMOKE=stand/arm64-smoke
ARM_FULL=stand/arm64-full
VIVA_INSTALL=stand/mipsle-install

# Куда идут строки журнала: report занимает stdout под сводку и переключает на stderr
LOG_FD=1

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

# vX.Y.Z-rc.N -> vX.Y.Z
base_of() {
  echo "${1%-"${PRE_KIND}".*}"
}

# vX.Y.Z-rc.N -> N
pre_n() {
  echo "${1##*-"${PRE_KIND}".}"
}

# Новейший пре-релиз базы $1, достижимый из HEAD (или пусто)
latest_pre() {
  local base="$1"
  { git tag -l "${base}-${PRE_KIND}.*" --merged HEAD | grep -E -- "$PRE_RE" | sort -V | tail -1; } || true
}

version_gt() {
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$1" ]
}

# Базы пре-релизов, достижимых из HEAD, строго новее последнего stable, по возрастанию
live_bases() {
  local last base
  last=$(last_stable)
  { git tag -l "v*-${PRE_KIND}.*" --merged HEAD | grep -E -- "$PRE_RE" | sed -E "s/-${PRE_KIND}\\.[0-9]+\$//" | sort -uV; } |
    while read -r base; do
      if [ -z "$last" ] || version_gt "$base" "$last"; then
        echo "$base"
      fi
    done
}

# Пре-релиз больше не нужен: есть stable его базы, пре-релиз той же базы с большим номером
# или любой тег более новой версии
rc_superseded() {
  local tag="$1" base n other obase
  base=$(base_of "$tag")
  n=$(pre_n "$tag")
  tag_exists "$base" && return 0
  for other in $(git tag -l 'v*' | grep -E "$STABLE_RE|$PRE_RE" || true); do
    obase=$(base_of "$other")
    if [ "$obase" = "$base" ] && [ "$other" != "$base" ] && [ "$(pre_n "$other")" -gt "$n" ]; then
      return 0
    fi
    version_gt "${obase#v}" "${base#v}" && return 0
  done
  return 1
}

heal() {
  local since tag
  since=$((NOW - HEAL_DAYS * 24 * 3600))
  for tag in $(git tag -l 'v*' | grep -E "$STABLE_RE|$PRE_RE" | sort -V || true); do
    [ "$(tag_time "$tag")" -ge "$since" ] || continue
    grep -qxF -- "$tag" <<<"${RELEASED:-}" && continue
    if [[ "$tag" =~ $PRE_RE ]] && rc_superseded "$tag"; then
      echo "delete $tag"
    else
      echo "publish $tag"
    fi
  done
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
      rc=$(latest_pre "$next")
      if [ -n "$rc" ] && ! release_worthy "$rc..HEAD"; then
        echo "rc $rc"
        return
      fi
      n=0
      [ -z "$rc" ] || n=$(pre_n "$rc")
      echo "rc $next-${PRE_KIND}.$((n + 1))"
      ;;
    *) echo "stable $next" ;;
  esac
}

# ── Состояние GitHub ──

log() { echo "auto-release: $*" >&"$LOG_FD"; }
# Для функций, чей stdout читает вызывающий ($(…))
logerr() { echo "auto-release: $*" >&2; }

# Проверка входов ручного запуска: PROMOTE_BASE — только vX.Y.Z; urgent — только вместе с ним.
# Значение PROMOTE_BASE в журнал не печатается (входы приходят от пользователей).
check_inputs() {
  URGENT_BASE=""
  if [ -n "${PROMOTE_BASE:-}" ] && ! [[ "$PROMOTE_BASE" =~ $STABLE_RE ]]; then
    log "promote_base не похож на vX.Y.Z — игнорируется"
    PROMOTE_BASE=""
  fi
  if [ "${URGENT:-0}" = 1 ]; then
    if [ -n "${PROMOTE_BASE:-}" ]; then
      URGENT_BASE="$PROMOTE_BASE"
      log "срочное: $PROMOTE_BASE"
    else
      URGENT=0
      log "срочное: без promote_base не действует"
    fi
  else
    URGENT=0
  fi
  export PROMOTE_BASE URGENT URGENT_BASE
}

# Стоп выпуска (D-14): переменная RELEASE_STOP или открытый issue с меткой release-stop,
# последнюю установку которой сделал пользователь с правом записи. Пусто в STOP_REASON — стопа нет.
# Ошибка чтения — стоп (fail closed).
compute_stop() {
  local val issues n login perm
  STOP_REASON=""

  if [ -z "${RELEASE_STOP+x}" ]; then
    # Запуск на ПК: переменная окружения не задана, читаем переменную репозитория
    if ! val=$(gh variable list --json name,value --jq '.[] | select(.name == "RELEASE_STOP") | .value'); then
      STOP_REASON="не удалось прочитать переменную RELEASE_STOP"
      return
    fi
  else
    val="$RELEASE_STOP"
  fi
  val="${val//[[:space:]]/}"
  case "${val,,}" in
    '' | 0 | false) ;;
    *)
      STOP_REASON="переменная RELEASE_STOP"
      return
      ;;
  esac

  if ! issues=$(gh api "repos/{owner}/{repo}/issues?state=open&labels=${STOP_LABEL}&per_page=20" \
    --jq '.[] | select(.pull_request | not) | .number'); then
    STOP_REASON="не удалось проверить стоп-метки"
    return
  fi
  for n in $issues; do
    [[ "$n" =~ ^[0-9]+$ ]] || continue
    if ! login=$(gh api --paginate "repos/{owner}/{repo}/issues/$n/events?per_page=100" \
      --jq ".[] | select(.event == \"labeled\" and .label.name == \"${STOP_LABEL}\") | .actor.login" | tail -1); then
      STOP_REASON="не удалось проверить стоп-метки"
      return
    fi
    if ! [[ "$login" =~ ^[A-Za-z0-9][A-Za-z0-9-]*(\[bot\])?$ ]]; then
      log "метка ${STOP_LABEL} на issue #$n: не удалось определить, кто её поставил — не засчитана"
      continue
    fi
    if perm=$(gh api "repos/{owner}/{repo}/collaborators/$login/permission" --jq .permission 2>/dev/null); then
      case "$perm" in
        admin | maintain | write)
          STOP_REASON="issue #$n (метка от $login)"
          return
          ;;
      esac
    elif [ "$login" = "$REPO_OWNER" ]; then
      # Эндпоинт прав недоступен токену: владельцу репозитория верим без проверки
      STOP_REASON="issue #$n (метка от $login)"
      return
    fi
    log "метка ${STOP_LABEL} на issue #$n поставлена пользователем без права записи — не засчитана"
  done
}

# Экспортирует RELEASED (теги всех релизов, включая draft), RELEASED_AT (строки "<тег> <unix
# publishedAt>", только не draft), STOP_REASON и REPO_OWNER. Ошибки gh не глушатся.
collect_state() {
  RELEASED=$(gh release list --limit 100 --json tagName --jq '.[].tagName')
  RELEASED_AT=$(gh release list --limit 100 --json tagName,publishedAt,isDraft \
    --jq '.[] | select((.isDraft | not) and .publishedAt != null) | "\(.tagName) \(.publishedAt | fromdateiso8601)"')
  if [ -n "${GITHUB_REPOSITORY_OWNER:-}" ]; then
    REPO_OWNER="$GITHUB_REPOSITORY_OWNER"
  else
    REPO_OWNER=$(gh repo view --json owner --jq .owner.login)
  fi
  compute_stop
  export RELEASED RELEASED_AT STOP_REASON REPO_OWNER
}

# Последний статус каждого контекста stand/* на коммите: строки "<контекст> <состояние>".
# Список статусов идёт от новых к старым, unique_by оставляет первый — последний по времени.
# Комбинированный /status не используется: его краснит любой чужой контекст.
stand_state() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]] || return 1
  gh api "repos/{owner}/{repo}/commits/$1/statuses?per_page=100" \
    --jq '[.[] | select(.context | startswith("stand/"))] | unique_by(.context) | .[] | "\(.context) \(.state)"'
}

# Вердикт по пре-релизу: "<ok|wait|red|stop|skip> <причина>[; предупреждение: …]"
verdict() {
  local tag="$1" base sha at st line ctx state red="" smoke=0 viva=0 age need reason
  base=$(base_of "$tag")
  if tag_exists "$base" || grep -qxF -- "$base" <<<"${RELEASED:-}"; then
    echo "skip stable $base уже выпущен"
    return
  fi
  at=$(awk -v t="$tag" '$1 == t { print $2 }' <<<"${RELEASED_AT:-}")
  if [ -z "$at" ]; then
    echo "wait не опубликован"
    return
  fi
  if [ -n "${STOP_REASON:-}" ]; then
    echo "stop $STOP_REASON"
    return
  fi
  sha=$(git rev-list -n1 "$tag")
  if ! st=$(stand_state "$sha"); then
    echo "wait не удалось прочитать статусы"
    return
  fi
  while read -r ctx state; do
    case "$ctx" in
      "$ARM_SMOKE" | "$ARM_FULL")
        [ "$state" != success ] || smoke=1
        ;;
      "$VIVA_INSTALL") viva=1 ;;
      *) continue ;;
    esac
    case "$state" in
      failure | error) red="${red:+$red, }$ctx" ;;
    esac
  done <<<"$st"
  if [ -n "$red" ]; then
    echo "red на стенде красный: $red"
    return
  fi
  if [ "$smoke" = 0 ]; then
    echo "wait ждёт смоука: make router-test SUITE=smoke RELEASE=$tag"
    return
  fi
  case "$base" in
    *.0)
      if [ "$base" = "${PROMOTE_BASE:-}" ]; then
        reason="выдержка отменена promote_base"
      else
        age=$((NOW - at))
        need=$((SOAK_HOURS * 3600))
        if [ "$age" -lt "$need" ]; then
          echo "wait выдержка $((age / 3600)) ч из $SOAK_HOURS"
          return
        fi
        reason="выдержка истекла"
      fi
      ;;
    *) reason="патч без выдержки" ;;
  esac
  if [ "$viva" = 0 ]; then
    reason="$reason; предупреждение: нет записи установки на Viva"
  fi
  echo "ok $reason"
}

# Кандидаты — по одному на базу, по возрастанию; продвигается первый подходящий, остальные
# оцениваются для сводки. Новая фаза выдержку прежнего пре-релиза не сбрасывает.
promote() {
  local base tag v kind found="" bases last
  : >"$CAND_FILE"
  bases=$(live_bases)
  if [ -z "$bases" ]; then
    last=$(last_stable)
    echo "none нет пре-релизов новее ${last:-начала истории}"
    return
  fi
  for base in $bases; do
    tag=$(latest_pre "$base")
    [ -n "$tag" ] || continue
    v=$(verdict "$tag")
    printf '%s\t%s\n' "$tag" "$v" >>"$CAND_FILE"
    logerr "кандидат $tag: $v"
    read -r kind _ <<<"$v"
    if [ "$kind" = ok ] && [ -z "$found" ]; then
      found="stable $base $tag"
    fi
  done
  echo "${found:-none ни один пре-релиз не готов к stable}"
}

summary_md() {
  local tag v
  echo "## Автовыпуск"
  echo
  echo "- Стоп выпуска: ${STOP_REASON:-нет}"
  echo "- Срочное: ${URGENT_BASE:-нет}"
  echo
  if [ -s "$CAND_FILE" ]; then
    echo "| Пре-релиз | Вердикт |"
    echo "|---|---|"
    while IFS=$'\t' read -r tag v; do
      echo "| $tag | $v |"
    done <"$CAND_FILE"
  else
    echo "Пре-релизов новее последнего stable нет."
  fi
  echo
  echo "- promote: ${PROMOTE_DECISION:-не принималось}"
  echo "- plan: ${PLAN_DECISION:-не принималось}"
}

# Только чтение: вердикты, promote и plan без публикации (ПК, проверка до слияния)
report() {
  LOG_FD=2
  check_inputs
  collect_state
  log "стоп выпуска: ${STOP_REASON:-нет}"
  PROMOTE_DECISION=$(promote)
  log "promote: $PROMOTE_DECISION"
  PLAN_DECISION=$(plan)
  log "plan: $PLAN_DECISION"
  summary_md
}

# ── Всё ниже нужно только в CI (gh, push, dispatch) ──

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

# Версии, для которых выдержка RC отменена: закрытая GitHub-веха vX.Y.Z или ручной
# запуск с promote_base. Отмена действует, только если RC этой версии уже опубликован
# как релиз (не draft); так закрытая веха не выпустит stable из недособранного RC.
waived_bases() {
  local candidates title rc
  candidates=$(gh api "repos/{owner}/{repo}/milestones?state=closed&per_page=100" --jq '.[].title' 2>/dev/null || true)
  candidates="$candidates"$'\n'"${PROMOTE_BASE:-}"
  for title in $(grep -E "$STABLE_RE" <<<"$candidates" | sort -u || true); do
    rc=$(latest_pre "$title")
    [ -n "$rc" ] || continue
    if [ "$(gh release view "$rc" --json isDraft --jq .isDraft 2>/dev/null || true)" = false ]; then
      echo "$title"
    fi
  done
}

# Коммит для тега stable. Обычно это коммит RC (те же байты, что выдержал RC). GitHub не даёт
# токену Actions создать тег на коммите, чьи workflow-файлы отличаются от main (нужен scope
# workflows), поэтому при изменении .github/workflows после RC тег ставится на HEAD: после RC
# там только chore/docs/test/ci (promote проверил, что feat/fix после него нет), бинарник
# они не меняют.
stable_sha() {
  local sha
  sha=$(git rev-list -n1 "$1")
  if git diff --quiet "$sha" HEAD -- .github/workflows; then
    echo "$sha"
  else
    git rev-parse HEAD
  fi
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
    echo "::error::сборка $tag упала $failed раза, нужен ручной разбор: gh run list --workflow build.yml --branch $tag"
    exit 1
  else
    if [ "$failed" -ge 1 ]; then
      echo "::warning::сборка $tag упала, повторяю"
    fi
    gh workflow run build.yml --ref "$tag" -f version="${tag#v}"
    log "запущена сборка $tag"
  fi
}

# Упавшая сборка не должна теряться: вытесненный RC без релиза удаляется с
# предупреждением в сводке прогона, актуальный тег пересобирается через publish.
heal_tags() {
  local action tag
  while read -r action tag; do
    case "$action" in
      delete)
        if [ "${DRY_RUN:-0}" = 1 ]; then
          log "DRY_RUN: удалил бы вытесненный неопубликованный $tag"
          continue
        fi
        echo "::warning::$tag вытеснен более новым релизом и не был опубликован (сборка: gh run list --workflow build.yml --branch $tag) — тег удалён"
        git push origin --delete "refs/tags/$tag" || true
        git tag -d "$tag" >/dev/null
        ;;
      publish)
        log "heal: у $tag нет релиза"
        publish "$tag" "$(git rev-list -n1 "$tag")"
        ;;
    esac
  done < <(heal)
}

run() {
  if [ "${GITHUB_ACTIONS:-}" = true ]; then
    git config user.name "github-actions"
    git config user.email "github-actions@github.com"
  fi

  local decision kind tag rc main_sha state
  collect_state
  heal_tags

  # «Нечего продвигать» — штатный ответ почти каждого запуска; в журнал он идёт
  # только вместе с итоговым решением, а не в каждом из промежуточных прогонов
  SOAK_WAIVED=$(waived_bases | tr '\n' ' ')
  export SOAK_WAIVED
  [ -z "${SOAK_WAIVED// /}" ] || log "выдержка RC отменена для: $SOAK_WAIVED"
  decision=$(promote)
  read -r kind tag rc <<<"$decision"
  if [ "$kind" = stable ]; then
    log "promote: $decision"
    publish "$tag" "$(stable_sha "$rc")"
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
      log "ждём остальные прогоны CI для ${main_sha:0:8}: решение примет последний из них"
      return
      ;;
  esac
  [ "$kind" = stable ] || log "promote: ${decision#none }"

  decision=$(plan)
  log "plan: $decision"
  read -r kind tag _ <<<"$decision"
  case "$kind" in
    stable | rc) publish "$tag" "$(git rev-list -n1 "$tag" 2>/dev/null || echo "$main_sha")" ;;
  esac
}

CAND_FILE=$(mktemp)
trap 'rm -f "$CAND_FILE"' EXIT

case "${1:-}" in
  plan) plan ;;
  promote)
    check_inputs
    collect_state
    promote
    ;;
  heal)
    [ -n "${RELEASED+x}" ] || collect_state
    heal
    ;;
  stable-sha) stable_sha "${2:?нужен тег RC}" ;;
  report) report ;;
  run)
    check_inputs
    run
    ;;
  *)
    sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
