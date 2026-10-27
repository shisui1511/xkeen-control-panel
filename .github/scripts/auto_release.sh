#!/usr/bin/env bash
# Автоматический выпуск пре-релизов и стабильных релизов (workflow auto-release.yml).
#
#   auto_release.sh plan     какой пре-релиз выпустить для HEAD (последний коммит main):
#                              "rc vX.Y.Z-beta.N" — новый пре-релиз (или уже существующий: ответ идемпотентен)
#                              "none <причина>"  — выпускать нечего
#                            слово rc в строке решения — тип решения «пре-релиз», не суффикс тега
#                            stable plan не выпускает: патч тоже идёт через пре-релиз
#   auto_release.sh promote  какой пре-релиз пора сделать стабильным:
#                              "stable vX.Y.Z vX.Y.Z-beta.N" / "none <причина>";
#                            вердикты кандидатов печатаются в stderr
#   auto_release.sh heal     свежие (7 дней) теги релизов без опубликованного релиза:
#                              "delete vX.Y.Z-beta.N" — пре-релиз вытеснен (stable базы, более новый
#                                                     stable или пре-релиз той же базы с большим номером)
#                              "publish vX.Y.Z[-beta.N]" — актуальный тег, сборку повторить
#   auto_release.sh report   только чтение GitHub и git: вердикты по каждому пре-релизу новее
#                            последнего stable, итог promote и plan, markdown-сводка в stdout;
#                            ничего не меняет (можно запускать на ПК до слияния)
#   auto_release.sh finalize тела релизов за 14 суток, последнего stable и новейших пре-релизов
#                            живых баз: блок xcp-release в первой строке и строка о регрессии у
#                            красного пре-релиза; с DRY_RUN=1 только печатает (ПК)
#   auto_release.sh run      полный цикл в CI: heal, promote, затем plan для зелёного main;
#                            ставит тег и запускает build.yml через workflow_dispatch
#                            (тег, запушенный GITHUB_TOKEN, сам сборку не запускает), затем finalize
#   auto_release.sh marker vX.Y.Z[-beta.N]
#                            строка блока xcp-release для заметок создаваемого релиза (build.yml):
#                            urgent из аннотации тега, hold — стоп выпуска; стоп не прочитан —
#                            hold:true
#
# Пре-релиз идёт в stable, только когда выполнено всё сразу: релиз опубликован (не draft); stable
# его базы ещё нет; стоп выключен; на коммите пре-релиза последний статус stand/arm64-smoke или
# stand/arm64-full — success; ни один последний статус stand/arm64-smoke, stand/arm64-full,
# stand/mipsle-install не failure и не error (красный смоук снимает более поздний success
# полного прогона, обратное не действует); у базы X.Y.0 истекла выдержка SOAK_HOURS от публикации
# (promote_base её снимает; при SOAK_HOURS=milestone выдержка длится до закрытия вехи и снимает её
# только promote_base), у базы X.Y.Z с Z>0 выдержки нет. Нет записи stand/mipsle-install —
# только предупреждение. Новая фаза выдержку прежнего пре-релиза не сбрасывает: кандидаты
# перебираются по базам по возрастанию, сбрасывает только пре-релиз той же базы. Тег stable
# ставится на коммит самого пре-релиза.
#
# Контракт для панелей (читает панель, Phase 145.3): первая строка тела каждого релиза за 14 суток,
# а также последнего stable и новейшего пре-релиза каждой живой базы при любом возрасте —
#   <!-- xcp-release: {"urgent":true|false,"hold":true|false} -->
# JSON без пробелов, ключи в этом порядке. urgent постоянный: у тега в аннотации строка
# "xcp-urgent: 1". hold динамический: включён стоп выпуска (если стоп не удалось прочитать, тела
# в этом прогоне не правятся, hold остаётся прежним). Новый релиз получает блок уже при создании
# (build.yml, команда marker): там непрочитанный стоп пишется как hold:true, чтобы релиз не ушёл
# в автоустановку до первого finalize с прочитанным стопом. Сроки автоустановки панель считает от
# published_at релиза, в блок они не пишутся. У пре-релиза новее последнего stable с красным
# статусом стенда после блока стоит строка о найденной регрессии (без подробностей стенда).
#
# Окружение:
#   SOAK_HOURS   сколько часов пре-релиз X.Y.0 должен провисеть после публикации (по умолчанию 24);
#                milestone — до закрытия вехи: базу X.Y.0 продвигает только PROMOTE_BASE
#   PRE_KIND     суффикс пре-релиза (по умолчанию beta): теги vX.Y.Z-<PRE_KIND>.N
#   PROMOTE_BASE версия vX.Y.Z из ручного запуска: снимает выдержку этой базы; готовая база
#                продвигается раньше младших, неготовая — предупреждение в журнале
#   URGENT=1     срочное: действует только вместе с PROMOTE_BASE, смоук и красные записи не
#                обходит; в аннотацию тега stable пишется строка "xcp-urgent: 1"
#   RELEASE_STOP стоп выпуска: непустое значение, кроме 0 и false (переменная репозитория);
#                на ПК, если переменная окружения не задана, читается из репозитория
#   RELEASE_TOKEN необязательный токен для тега stable на коммите, где .github/workflows
#                отличается от main (токен Actions такой тег не поставит)
#   DRY_RUN=1    run только печатает решения
#   RUN_SHA      коммит, чей CI завершился (workflow_run); пусто — берётся main
#   NOW          время решения, unix

set -euo pipefail

SOAK_HOURS="${SOAK_HOURS:-24}"
[[ "$SOAK_HOURS" =~ ^([0-9]+|milestone)$ ]] || SOAK_HOURS=24
NOW="${NOW:-$(date +%s)}"
PRE_KIND="${PRE_KIND:-beta}"
[[ "$PRE_KIND" =~ ^[a-z]+$ ]] || {
  echo "auto-release: PRE_KIND должен состоять из строчных латинских букв" >&2
  exit 2
}

# Коммиты, ради которых выходит релиз; chore/docs/test/ci бинарник не меняют
WORTHY='^(feat|fix|perf|refactor|revert)(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE'
STABLE_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'
PRE_RE="^v[0-9]+\\.[0-9]+\\.[0-9]+-${PRE_KIND}\\.[0-9]+\$"
HEAL_DAYS=7
HOLD_DAYS=14
REGRESSION_LINE='> На стенде найдена регрессия, исправление выйдет следующим пре-релизом.'
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

# vX.Y.Z-beta.N -> vX.Y.Z
base_of() {
  echo "${1%-"${PRE_KIND}".*}"
}

# vX.Y.Z-beta.N -> N
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
  { git tag -l "v*-${PRE_KIND}.*" --merged HEAD | grep -E -- "$PRE_RE" | sed -E "s/-${PRE_KIND}\\.[0-9]+\$//" | sort -uV || true; } |
    while read -r base; do
      if [ -z "$last" ] || version_gt "$base" "$last"; then
        echo "$base"
      fi
    done
}

# Пре-релиз больше не нужен: есть stable его базы, более новый stable или пре-релиз той же базы
# с большим номером. Пре-релиз более новой базы старый не вытесняет: старая база ещё может стать
# stable; более новый stable вытесняет — пре-релиз ниже текущего stable не публикуется.
rc_superseded() {
  local tag="$1" base n other newest
  base=$(base_of "$tag")
  n=$(pre_n "$tag")
  tag_exists "$base" && return 0
  newest=$({ git tag -l 'v*' | grep -E "$STABLE_RE" | sort -V | tail -1; } || true)
  if [ -n "$newest" ] && ! version_gt "$base" "$newest"; then
    return 0
  fi
  for other in $(git tag -l "${base}-${PRE_KIND}.*" | grep -E "$PRE_RE" || true); do
    if [ "$(pre_n "$other")" -gt "$n" ]; then
      return 0
    fi
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

# Что выпустить для HEAD. Stable не выпускается никогда: он выходит только из пре-релиза
# (promote), патчи тоже идут через пре-релиз.
#   нет пре-релизов новее последнего stable      → первый пре-релиз версии по scripts/version.sh --next
#   после последнего пре-релиза базы B нет изменений → он же (идемпотентно)
#   после него feat или ломающее изменение       → новая база: минор B + 1 (major + 1 для ломающего при major ≥ 1)
#   после него только fix/perf/refactor/revert    → следующий номер пре-релиза базы B
plan() {
  local last range next B tip log n major minor
  last=$(last_stable)
  range="${last:+$last..}HEAD"
  if ! release_worthy "$range"; then
    echo "none нет feat/fix/perf/refactor после ${last:-начала истории}"
    return
  fi

  B=$(live_bases | tail -1)
  if [ -z "$B" ]; then
    next=$(sh "$ROOT/scripts/version.sh" --next)
    echo "rc $next-${PRE_KIND}.1"
    return
  fi

  tip=$(latest_pre "$B")
  if ! release_worthy "$tip..HEAD"; then
    echo "rc $tip"
    return
  fi
  log=$(git log --format='%s%n%b' "$tip..HEAD")
  if grep -qE '^feat(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE' <<<"$log"; then
    major=${B#v}
    major=${major%%.*}
    minor=${B#v*.}
    minor=${minor%%.*}
    if [ "$major" -ge 1 ] && grep -qE '^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE' <<<"$log"; then
      echo "rc v$((major + 1)).0.0-${PRE_KIND}.1"
    else
      echo "rc v$major.$((minor + 1)).0-${PRE_KIND}.1"
    fi
    return
  fi
  n=$(pre_n "$tip")
  echo "rc $B-${PRE_KIND}.$((n + 1))"
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

# Стоп выпуска: переменная RELEASE_STOP или открытый issue с меткой release-stop, последнюю
# установку которой сделал пользователь с правом записи. Пусто в STOP_REASON — стопа нет.
# Ошибка чтения — стоп (fail closed).
compute_stop() {
  local val issues n login perm err
  STOP_REASON=""
  STOP_UNKNOWN=0

  if [ -z "${RELEASE_STOP+x}" ]; then
    # Запуск на ПК: переменная окружения не задана, читаем переменную репозитория
    if ! val=$(gh variable list --json name,value --jq '.[] | select(.name == "RELEASE_STOP") | .value'); then
      STOP_UNKNOWN=1
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
    STOP_UNKNOWN=1
    STOP_REASON="не удалось проверить стоп-метки"
    return
  fi
  for n in $issues; do
    [[ "$n" =~ ^[0-9]+$ ]] || continue
    if ! login=$(gh api --paginate "repos/{owner}/{repo}/issues/$n/events?per_page=100" \
      --jq ".[] | select(.event == \"labeled\" and .label.name == \"${STOP_LABEL}\") | .actor.login" | tail -1); then
      STOP_UNKNOWN=1
      STOP_REASON="не удалось проверить стоп-метки"
      return
    fi
    if ! [[ "$login" =~ ^[A-Za-z0-9][A-Za-z0-9-]*(\[bot\])?$ ]]; then
      log "метка ${STOP_LABEL} на issue #$n: не удалось определить, кто её поставил — не засчитана"
      continue
    fi
    err=$(mktemp)
    if perm=$(gh api "repos/{owner}/{repo}/collaborators/$login/permission" --jq .permission 2>"$err"); then
      rm -f "$err"
      case "$perm" in
        admin | maintain | write)
          STOP_REASON="issue #$n (метка от $login)"
          return
          ;;
      esac
    elif [ "$login" = "$REPO_OWNER" ]; then
      rm -f "$err"
      # Эндпоинт прав недоступен токену: владельцу репозитория верим без проверки
      STOP_REASON="issue #$n (метка от $login)"
      return
    elif ! grep -q 'HTTP 404' "$err"; then
      rm -f "$err"
      # 404 — не коллаборатор; любая другая ошибка (5xx, лимит, тайм-аут) — стоп (fail closed)
      STOP_UNKNOWN=1
      STOP_REASON="не удалось проверить права на стоп-метку issue #$n"
      return
    else
      rm -f "$err"
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
  export RELEASED RELEASED_AT STOP_REASON STOP_UNKNOWN REPO_OWNER
}

# Последний статус каждого контекста stand/* на коммите: строки "<контекст> <состояние> <время>".
# Список статусов идёт от новых к старым, unique_by оставляет первый — последний по времени.
# Комбинированный /status не используется: его краснит любой чужой контекст.
stand_state() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]] || return 1
  gh api "repos/{owner}/{repo}/commits/$1/statuses?per_page=100" \
    --jq '[.[] | select(.context | startswith("stand/"))] | unique_by(.context) | .[] | "\(.context) \(.state) \(.created_at)"'
}

# Красные контексты по строкам stand_state (аргумент), через запятую; пусто — красных нет.
# Красный смоук снимает более поздний success полного прогона на том же коммите: полный прогон
# включает смоук. Обратное не действует: зелёный смоук красный полный прогон не снимает.
# Время ISO 8601 в UTC, сравнивается как строка.
stand_red() {
  local ctx state ts red="" smoke_state="" smoke_ts="" full_state="" full_ts=""
  while read -r ctx state ts; do
    case "$ctx" in
      "$ARM_SMOKE")
        smoke_state="$state"
        smoke_ts="$ts"
        ;;
      "$ARM_FULL")
        full_state="$state"
        full_ts="$ts"
        ;;
      "$VIVA_INSTALL")
        case "$state" in failure | error) red="${red:+$red, }$ctx" ;; esac
        ;;
    esac
  done <<<"$1"
  case "$smoke_state" in
    failure | error)
      if ! { [ "$full_state" = success ] && [[ "$full_ts" > "$smoke_ts" ]]; }; then
        red="${red:+$red, }$ARM_SMOKE"
      fi
      ;;
  esac
  case "$full_state" in failure | error) red="${red:+$red, }$ARM_FULL" ;; esac
  printf '%s' "$red"
}

# Вердикт по пре-релизу: "<ok|wait|red|stop|skip> <причина>[; предупреждение: …]"
verdict() {
  local tag="$1" base sha at st ctx state ts red="" smoke=0 viva=0 age need reason
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
  while read -r ctx state ts; do
    case "$ctx" in
      "$ARM_SMOKE" | "$ARM_FULL")
        [ "$state" != success ] || smoke=1
        ;;
      "$VIVA_INSTALL") viva=1 ;;
    esac
  done <<<"$st"
  red=$(stand_red "$st")
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
      elif [ "$SOAK_HOURS" = milestone ]; then
        echo "wait выдержка до закрытия вехи (promote_base)"
        return
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
# оцениваются для сводки. Новая фаза выдержку прежнего пре-релиза не сбрасывает. База из
# PROMOTE_BASE с вердиктом ok идёт первой: разовый вход ручного запуска (и пометка «срочное»)
# не теряется из-за того, что младшая база готова раньше.
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
    if [ "$kind" = ok ] && { [ -z "$found" ] || [ "$base" = "${PROMOTE_BASE:-}" ]; }; then
      found="stable $base $tag"
    fi
  done
  if [ -n "${PROMOTE_BASE:-}" ]; then
    read -r _ base _ <<<"${found:-none - -}"
    if [ "$base" != "$PROMOTE_BASE" ]; then
      logerr "::warning::promote_base $PROMOTE_BASE не применён: его пре-релиз не готов к stable (вердикт в сводке); ручной запуск нужно повторить"
    fi
  fi
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
  echo "- Тела релизов: ${FINALIZE_NOTE:-не обновлялись}"
  if [ -n "${BUILD_GAVE_UP:-}" ]; then
    echo "- Сборка сдалась после двух падений (нужен ручной разбор): $BUILD_GAVE_UP"
  fi
}

# Красный ли пре-релиз: база новее последнего stable и последний статус stand/arm64-smoke,
# stand/arm64-full или stand/mipsle-install на его коммите — failure/error. Печатает 1 или 0;
# ошибка чтения статусов — 0 с предупреждением в журнал.
tag_is_red() {
  local tag="$1" last sha st
  [[ "$tag" =~ $PRE_RE ]] || {
    echo 0
    return
  }
  last=$(last_stable)
  if [ -n "$last" ] && ! version_gt "$(base_of "$tag")" "$last"; then
    echo 0
    return
  fi
  if ! sha=$(git rev-list -n1 "$tag" 2>/dev/null); then
    logerr "::warning::тег $tag не найден локально, статусы стенда не прочитаны"
    echo 0
    return
  fi
  if ! st=$(stand_state "$sha" 2>/dev/null); then
    logerr "::warning::не удалось прочитать статусы стенда для $tag"
    echo 0
    return
  fi
  if [ -n "$(stand_red "$st")" ]; then
    echo 1
  else
    echo 0
  fi
}

# Тела релизов за HOLD_DAYS суток, а также последнего stable и новейшего пре-релиза каждой живой
# базы при любом возрасте (стоп должен дойти и до старого stable): блок xcp-release (urgent, hold)
# в первой строке и, у красного пре-релиза, строка о регрессии. Прежние строки блока и регрессии
# удаляются перед вставкой; gh release edit только при отличии тела (идемпотентно). Ошибка правки
# одного релиза — предупреждение.
finalize() {
  local tag at urgent hold red body cleaned new tmp total=0 changed=0 since mark always b
  # Стоп не прочитан (сбой gh): продвижение он держит, но в публичный контракт hold:true по
  # непроверенному стопу не пишется — тела не трогаются до следующего прогона
  if [ "${STOP_UNKNOWN:-0}" = 1 ]; then
    FINALIZE_NOTE="пропущено: стоп не прочитан"
    log "тела релизов не обновлялись: стоп не прочитан"
    return
  fi
  since=$((NOW - HOLD_DAYS * 24 * 3600))
  always=$(
    last_stable
    for b in $(live_bases); do latest_pre "$b"; done
  )
  while read -r tag at; do
    [ -n "$tag" ] || continue
    [ "$at" -ge "$since" ] || grep -qxF -- "$tag" <<<"$always" || continue
    total=$((total + 1))
    if git for-each-ref --format='%(contents)' "refs/tags/$tag" | grep -qxF 'xcp-urgent: 1'; then
      urgent=true
    else
      urgent=false
    fi
    if [ -n "${STOP_REASON:-}" ]; then hold=true; else hold=false; fi
    red=$(tag_is_red "$tag")
    if ! body=$(gh release view "$tag" --json body --jq .body </dev/null); then
      echo "::warning::тело $tag не прочитано"
      continue
    fi
    body=$(printf '%s\n' "$body" | tr -d '\r')
    cleaned=$(printf '%s\n' "$body" |
      awk -v reg="$REGRESSION_LINE" 'index($0, "<!-- xcp-release:") == 1 || $0 == reg { next } { print }' |
      sed '/./,$!d')
    new="<!-- xcp-release: {\"urgent\":${urgent},\"hold\":${hold}} -->"
    if [ "$red" = 1 ]; then
      new="${new}"$'\n'"${REGRESSION_LINE}"
    fi
    new="${new}"$'\n\n'"${cleaned}"
    # $(…) отбрасывает хвостовые переводы строк; хвостовые пробелы тоже не считаются отличием
    new="${new%"${new##*[![:space:]]}"}"
    body="${body%"${body##*[![:space:]]}"}"
    [ "$new" != "$body" ] || continue
    if [ "$red" = 1 ]; then mark=да; else mark=нет; fi
    if [ "${DRY_RUN:-0}" = 1 ]; then
      log "DRY_RUN: обновил бы тело $tag: urgent=$urgent hold=$hold регрессия=$mark"
      changed=$((changed + 1))
      continue
    fi
    tmp=$(mktemp)
    printf '%s\n' "$new" >"$tmp"
    if gh release edit "$tag" --notes-file "$tmp" </dev/null >/dev/null; then
      log "обновлено тело $tag: urgent=$urgent hold=$hold регрессия=$mark"
      changed=$((changed + 1))
    else
      echo "::warning::тело $tag не обновлено"
    fi
    rm -f "$tmp"
  done <<<"${RELEASED_AT:-}"
  FINALIZE_NOTE="обновлено $changed из $total (за ${HOLD_DAYS} суток и последние релизы баз)"
}

# Блок xcp-release для заметок создаваемого релиза $1 (build.yml, до gh release create): urgent —
# строка "xcp-urgent: 1" в аннотации тега, hold — стоп выпуска. Стоп не прочитан — hold:true (fail
# closed): иначе до прогона finalize свежий релиз читался бы панелями как hold:false. Журнал — в
# stderr, stdout занят строкой блока.
release_marker() {
  local tag="$1" urgent=false hold=false
  if ! [[ "$tag" =~ $STABLE_RE || "$tag" =~ $PRE_RE ]]; then
    logerr "marker: тег не похож на vX.Y.Z или vX.Y.Z-${PRE_KIND}.N"
    exit 2
  fi
  LOG_FD=2
  if git for-each-ref --format='%(contents)' "refs/tags/$tag" | grep -qxF 'xcp-urgent: 1'; then
    urgent=true
  fi
  if [ -n "${GITHUB_REPOSITORY_OWNER:-}" ]; then
    REPO_OWNER="$GITHUB_REPOSITORY_OWNER"
  elif ! REPO_OWNER=$(gh repo view --json owner --jq .owner.login); then
    REPO_OWNER=""
  fi
  compute_stop
  if [ -n "${STOP_REASON:-}" ]; then
    hold=true
    log "marker $tag: стоп — $STOP_REASON"
  fi
  printf '<!-- xcp-release: {"urgent":%s,"hold":%s} -->\n' "$urgent" "$hold"
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

# Ставит тег stable $1 на коммит пре-релиза $2 (никогда на другой коммит).
# Токен Actions не ставит тег на коммит, чьи .github/workflows отличаются от main (нужен scope
# workflows). Тогда тег уходит запасным токеном RELEASE_TOKEN; без него — ошибка с готовой
# командой владельцу. Токен нигде не печатается, трассировка (set -x) не включается.
# Коды: 0 — запушен токеном Actions; 10 — запушен RELEASE_TOKEN (сборку запустит сам push
# тега); 1 — поставить нельзя.
push_stable_tag() {
  local tag="$1" sha="$2"
  local -a msgs=(-m "$tag")
  if [ "${URGENT:-0}" = 1 ] && [ "$tag" = "${PROMOTE_BASE:-}" ]; then
    msgs+=(-m 'xcp-urgent: 1')
  fi
  git tag -a "${msgs[@]}" "$tag" "$sha"
  if git diff --quiet "$sha" HEAD -- .github/workflows; then
    if git push origin "refs/tags/$tag"; then
      return 0
    fi
    git tag -d "$tag" >/dev/null
    echo "::error::не удалось запушить тег $tag"
    return 1
  fi
  if [ -n "${RELEASE_TOKEN:-}" ]; then
    # Заголовок авторизации, оставленный actions/checkout, сбрасывается: иначе уйдёт токен Actions
    if git -c 'http.https://github.com/.extraheader=' push \
      "https://x-access-token:${RELEASE_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" "refs/tags/$tag"; then
      return 10
    fi
    git tag -d "$tag" >/dev/null
    echo "::error::не удалось запушить тег $tag запасным токеном"
    return 1
  fi
  git tag -d "$tag" >/dev/null
  # Подсказка из тех же сообщений, что и у тега выше: пометка «срочное» не теряется
  local hint="git tag -a -m $tag"
  if [ "${#msgs[@]}" -gt 2 ]; then
    hint="$hint -m 'xcp-urgent: 1'"
  fi
  echo "::error::workflows изменились после ${sha:0:8}; токен Actions тег не поставит. Владельцу: $hint $tag $sha && git push origin $tag"
  return 1
}

# Ставит тег (если его нет) и запускает сборку. Идемпотентно: при опубликованном релизе
# ничего не делает, при идущей сборке ждёт, после двух упавших сборок сдаётся: тег попадает в
# BUILD_GAVE_UP, а run доводит цикл до конца и выходит с ошибкой.
publish() {
  local tag="$1" sha="$2" active failed code
  if gh release view "$tag" >/dev/null 2>&1; then
    log "$tag уже опубликован"
    return
  fi
  if [ "${DRY_RUN:-0}" = 1 ]; then
    log "DRY_RUN: выпустил бы $tag на ${sha:0:8}"
    return
  fi
  if ! tag_exists "$tag"; then
    if [[ "$tag" =~ $STABLE_RE ]]; then
      code=0
      push_stable_tag "$tag" "$sha" || code=$?
      case "$code" in
        10)
          log "тег $tag поставлен на ${sha:0:8} запасным токеном, сборку запустит push тега"
          return
          ;;
        0) ;;
        *)
          STABLE_NOT_TAGGED=1
          return
          ;;
      esac
    else
      git tag -a "$tag" -m "$tag" "$sha"
      git push origin "refs/tags/$tag"
    fi
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
    # Не выход из скрипта: остальные базы, plan и finalize идут дальше, run завершится с ошибкой
    BUILD_GAVE_UP="${BUILD_GAVE_UP:+$BUILD_GAVE_UP, }$tag"
  else
    if [ "$failed" -ge 1 ]; then
      echo "::warning::сборка $tag упала, повторяю"
    fi
    gh workflow run build.yml --ref "$tag" -f version="${tag#v}"
    log "запущена сборка $tag"
  fi
}

# Упавшая сборка не должна теряться: вытесненный пре-релиз без релиза удаляется с
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

  local kind tag rc main_sha state=0
  BUILD_GAVE_UP=""
  collect_state
  heal_tags
  log "стоп выпуска: ${STOP_REASON:-нет}"

  # Продвижение пре-релиза в stable не зависит от состояния CI main: оно решается статусами
  # стенда на коммите самого пре-релиза
  PROMOTE_DECISION=$(promote)
  log "promote: $PROMOTE_DECISION"
  read -r kind tag rc <<<"$PROMOTE_DECISION"
  if [ "$kind" = stable ]; then
    STABLE_NOT_TAGGED=0
    publish "$tag" "$(git rev-list -n1 "$rc")"
    if [ "$STABLE_NOT_TAGGED" = 1 ]; then
      PROMOTE_DECISION="$PROMOTE_DECISION (тег не поставлен — нужен ручной шаг, см. ::error::)"
    fi
  fi

  # Новый пре-релиз — только для зелёного main; стоп его не держит. Сводка пишется при любом исходе
  main_sha=$(git rev-parse HEAD)
  if [ -n "${RUN_SHA:-}" ] && [ "$RUN_SHA" != "$main_sha" ]; then
    PLAN_DECISION="не принималось (CI завершился для ${RUN_SHA:0:8}, но main уже на ${main_sha:0:8} — решит прогон нового коммита)"
  else
    ci_state "$main_sha" || state=$?
    case "$state" in
      1) PLAN_DECISION="не принималось (CI для ${main_sha:0:8} красный)" ;;
      2) PLAN_DECISION="не принималось (ждём остальные прогоны CI для ${main_sha:0:8}: решение примет последний из них)" ;;
      *)
        PLAN_DECISION=$(plan)
        read -r kind tag _ <<<"$PLAN_DECISION"
        if [ "$kind" = rc ]; then
          publish "$tag" "$(git rev-list -n1 "$tag" 2>/dev/null || echo "$main_sha")"
        fi
        ;;
    esac
  fi
  log "plan: $PLAN_DECISION"

  # Релиз, опубликованный сборкой позже, блок получает при создании (marker в build.yml);
  # finalize обновит его в следующем прогоне
  finalize

  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    summary_md >>"$GITHUB_STEP_SUMMARY"
  fi
  if [ -n "${BUILD_GAVE_UP:-}" ]; then
    echo "::error::сборка сдалась после двух падений: $BUILD_GAVE_UP"
    exit 1
  fi
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
  report) report ;;
  marker) release_marker "${2:-}" ;;
  finalize)
    check_inputs
    collect_state
    finalize
    ;;
  run)
    check_inputs
    run
    ;;
  *)
    awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
    exit 2
    ;;
esac
