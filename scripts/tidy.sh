#!/bin/sh
# scripts/tidy.sh — уборка за агентами на ПК: worktree и ветки git, сборки, отчёты стенда, кэш релизов.
#
#   scripts/tidy.sh                  показ: что будет удалено и сколько места, ничего не меняется
#   scripts/tidy.sh --apply          выполнить план, построенный в этом же запуске (make tidy APPLY=1);
#                                    отдельный запуск показа план не фиксирует — состав мог измениться
#   scripts/tidy.sh --remote         добавить ветки origin, слитые в origin/main (сперва git fetch --prune);
#                                    --remote --apply удаляет их по одной (с проверкой вершины через
#                                    --force-with-lease) — только после «да» владельца на показанный
#                                    список (make tidy REMOTE=1); чтобы удалить ровно показанное,
#                                    повторить с --remote-branch <имя> на каждую ветку списка
#   scripts/tidy.sh --remote-branch <имя>  сузить ветки origin до названных (повторяемый; нужен --remote)
#   scripts/tidy.sh --rescue <worktree>    разово: спасти названный worktree (путь или имя каталога)
#                                    в rescue/<имя>; слитость и возраст не проверяются (повторяемый)
#   scripts/tidy.sh --force-branch <ветка> разово: удалить названную неслитую локальную ветку (повторяемый)
#   scripts/tidy.sh --drop-stash <stash@{N}=sha>  разово: удалить запись stash, только если её коммит
#                                    начинается с sha (7–40 символов из показа; номера сдвигаются) (повторяемый)
#   scripts/tidy.sh --drop-legacy    вместо ротации убрать весь старый формат build/router одним пунктом
#   scripts/tidy.sh --rotate-builds  только ротация сборок build/xcp_v*_<арх> (вызывает Makefile)
#   scripts/tidy.sh -h | --help      эта справка
#
# Разовые флаги (--rescue, --force-branch, --drop-stash) действуют только на названные объекты
# и в обычный --apply не входят: stash обычный запуск лишь показывает, неслитые ветки не трогает.
#
# Пороги — переменные XCP_TIDY_* (сборки, отчёты, кэш релизов — в шапке scripts/router/tidy-lib.sh);
# здесь XCP_TIDY_WORKTREE_HOURS — сколько часов правки worktree считаются свежими (24).
# Показ и исполнение строятся по одному плану: по категориям, с действием, путём,
# размером и пропущенным с причиной. Пути — от корня репозитория.
# Отчёты, кэш релизов и старый формат build/router трогаются, только пока общая
# блокировка стенда свободна (RT_LOCK_HELD=1 — её держит родительский прогон);
# иначе эти категории пропускаются с причиной «блокировка занята прогоном».
#
# Worktree: внутри каталога репозитория, слитые (вершина достижима из origin/main или из текущей
# ветки основной копии) и без правок моложе XCP_TIDY_WORKTREE_HOURS часов (24). Чистый — «снять
# worktree»; с незакоммиченным — «спасти»: WIP-коммит в локальную ветку rescue/<имя каталога>
# (без frontend/node_modules; индекс и файлы worktree не меняются), todo rescue-<имя>.md в
# .planning/todos/pending основной копии, затем git worktree remove и удаление ветки worktree.
# Ветки rescue/* только локальные: команда их не удаляет и не отправляет.
# Игнорируемые файлы (свой .planning, отчёты build/router/<отчёт>, *.test, журналы…): слитый
# worktree, а также названный --rescue, с ними вне воспроизводимого пропускается с причиной
# «игнорируемые файлы: N» — git worktree remove удалил бы их вместе с каталогом, а rescue-коммит
# их не берёт. Воспроизводимое снятию не мешает: frontend/node_modules, frontend/dist/, .go/,
# .go-cache/, build/xcp*, build/router/bin/, build/router/release/ и символические ссылки.
# Для --rescue полный список путей печатается в stderr, решение за владельцем: перенести нужное
# или удалить вручную и повторить. Игнорируемое никуда не копируется и новым флагом не снимается.
# Прочие worktree — пропуск с причиной: «заблокирован», «не слита», «правки моложе 24 ч»,
# «возраст не определён», «вне репозитория», «текущий», «блокировка занята прогоном»,
# «игнорируемые файлы: N», «игнорируемые файлы не прочитаны», «вложенный git-репозиторий».
#
# Локальные ветки: слитые (вершина достижима из origin/main или из текущей ветки основной копии)
# удаляются, кроме main, текущих, rescue/* (одна итоговая строка) и занятых worktree («занята
# worktree»); неслитые — «не слита». Ветки origin: без --remote категории нет и origin не
# трогается; слитость — только по origin/main; main и rescue/* не удаляются никогда, ветки по
# маскам XCP_TIDY_PROTECT_REMOTE (через запятую или пробел, шаблоны sh, например «release/*») —
# тоже; причины пропуска «не слита», «нет на origin», «нет связи с origin», «защищена».
# stash — пропуск «stash».
# В git: prune записей о пропавших worktree (git worktree prune) и обычный git gc последним
# действием (без --prune=now). Удалённые --force-branch ветка и --drop-stash запись в reflog не
# остаются: их коммиты держатся только как недостижимые объекты, а gc убирает те, что старше
# gc.pruneExpire. Поэтому с --force-branch и --drop-stash gc не выполняется, а полный sha
# удалённого печатается (вернуть: git branch <имя> <sha>; git stash store -m <тема> <sha>).
#
# Порядок --apply: worktree, prune, локальные ветки, ветки origin, stash, файловые категории, gc.
#
# Коды выхода: 0 — готово; 1 — часть операций не удалась; 2 — неверные аргументы.
set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/router/lib.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/router/tidy-lib.sh"

APPLY=0
ROTATE_BUILDS=0
DROP_LEGACY=0
REMOTE=0
RESCUE_LIST=""
FORCE_BRANCHES=""
DROP_STASHES=""
REMOTE_BRANCHES=""
while [ $# -gt 0 ]; do
  _a=$1
  case "$_a" in
    --apply) APPLY=1 ;;
    --rotate-builds) ROTATE_BUILDS=1 ;;
    --drop-legacy) DROP_LEGACY=1 ;;
    --remote) REMOTE=1 ;;
    --rescue | --force-branch | --drop-stash | --remote-branch)
      if [ $# -lt 2 ]; then
        echo "tidy: $_a требует значение" >&2
        exit 2
      fi
      case "$2" in
        '' | -*)
          echo "tidy: $_a: неверное значение: $2" >&2
          exit 2
          ;;
      esac
      case "$_a" in
        --rescue) RESCUE_LIST="$RESCUE_LIST$2
" ;;
        --force-branch) FORCE_BRANCHES="$FORCE_BRANCHES$2
" ;;
        --remote-branch) REMOTE_BRANCHES="$REMOTE_BRANCHES$2
" ;;
        --drop-stash)
          if ! printf '%s\n' "$2" | grep -Eq '^stash@\{[0-9]+\}=[0-9a-f]{7,40}$'; then
            echo "tidy: --drop-stash ожидает stash@{N}=<sha коммита из показа, 7–40 символов>: $2" >&2
            exit 2
          fi
          DROP_STASHES="$DROP_STASHES$2
"
          ;;
      esac
      shift
      ;;
    -h | --help)
      awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
      exit 0
      ;;
    *)
      echo "tidy: неизвестный аргумент: $_a" >&2
      exit 2
      ;;
  esac
  shift
done

if [ -n "$REMOTE_BRANCHES" ] && [ "$REMOTE" != 1 ]; then
  echo "tidy: --remote-branch требует --remote" >&2
  exit 2
fi

# Ротация сборок для Makefile: сборка не должна падать из-за уборки.
if [ "$ROTATE_BUILDS" = 1 ]; then
  if _root=$(rt_repo_root 2>/dev/null) && [ -n "$_root" ]; then
    tidy_rotate_builds "$_root" || echo "tidy: предупреждение: ротация сборок не завершилась" >&2
  fi
  exit 0
fi

ROOT=$(rt_repo_root)
cd "$ROOT"

PLAN=$(mktemp)
WT_LIST=$(mktemp)
WT_TMP=$(mktemp)
WT_IGN=$(mktemp)
trap 'rm -f "$PLAN" "$WT_LIST" "$WT_TMP" "$WT_IGN"' EXIT

# категории показа через "|" (в названиях есть пробелы), порядок = порядок показа
CATEGORIES="worktree|локальные ветки|"
if [ "$REMOTE" = 1 ]; then
  CATEGORIES="${CATEGORIES}ветки origin|"
fi
CATEGORIES="${CATEGORIES}stash|git|сборки|отчёты|кэш релизов|старый формат build/router|обрывки frontend/build/router|корневые хвосты"

# Общая блокировка стенда (Pitfall: в потомке run.sh fd 9 уже держит родитель, пробовать
# заново нельзя). Занята чужим прогоном — отчёты, кэш релизов и старый формат пропускаются.
LOCK_BUSY=0
if [ "${RT_LOCK_HELD:-0}" != 1 ]; then
  if command -v flock >/dev/null 2>&1; then
    exec 9>"$(rt_common_dir)/router-test.lock"
    if ! flock -n 9; then
      LOCK_BUSY=1
    fi
  else
    LOCK_BUSY=1
  fi
fi

# --- построение плана: одна функция на категорию, записи добавляются в $PLAN -----------

plan_builds() {
  tidy_plan_builds "$ROOT" >>"$PLAN"
}

plan_locked() { # <категория> <функция плана…>
  _pl_cat=$1
  shift
  if [ "$LOCK_BUSY" = 1 ]; then
    printf 'skip\t%s\t%s\t0\t%s\n' "$_pl_cat" "$ROOT/build/router" "блокировка занята прогоном" >>"$PLAN"
  else
    "$@" >>"$PLAN"
  fi
}

plan_reports() {
  plan_locked "отчёты" tidy_plan_reports "$ROOT"
}

plan_releases() {
  plan_locked "кэш релизов" tidy_plan_releases "$ROOT"
}

plan_legacy() {
  if [ "$DROP_LEGACY" = 1 ]; then
    plan_locked "старый формат build/router" tidy_plan_legacy_all "$ROOT"
  else
    plan_locked "старый формат build/router" tidy_plan_legacy "$ROOT"
  fi
}

# Обрывки frontend/build/router/<имя>: Playwright разрешил относительный путь отчёта от
# frontend/. Обрывок с одноимённым отчётом build/router/<имя> и без общих файлов
# переносится в отчёт, без отчёта удаляется, с общими файлами пропускается. state.json
# (cookie и CSRF закрытых сессий) удаляются, а не переносятся.
plan_stubs() {
  _ps_cat="обрывки frontend/build/router"
  _ps_dir="$ROOT/frontend/build/router"
  _ps_skipped=0
  [ -d "$_ps_dir" ] || return 0
  _ps_a=$(mktemp)
  _ps_b=$(mktemp)
  _ps_names=$(mktemp)
  LC_ALL=C ls -1A "$_ps_dir" >"$_ps_names"
  while IFS= read -r _ps_n; do
    _ps_stub="$_ps_dir/$_ps_n"
    if [ ! -d "$_ps_stub" ] || [ -L "$_ps_stub" ]; then
      _ps_skipped=1
      printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "не каталог отчёта" >>"$PLAN"
      continue
    fi
    _ps_rep="$ROOT/build/router/$_ps_n"
    if [ -d "$_ps_rep" ] && [ ! -L "$_ps_rep" ]; then
      find "$_ps_stub" -type f \( -name state.json -o -name state.json.restore.json \) |
        _tl_emit_paths rm "$_ps_cat" "сессия панели закрыта: cookie и CSRF не переносятся" >>"$PLAN"
      (cd "$_ps_stub" && find . -type f ! -name state.json ! -name state.json.restore.json | LC_ALL=C sort) >"$_ps_a"
      (cd "$_ps_rep" && find . -type f | LC_ALL=C sort) >"$_ps_b"
      if [ -n "$(LC_ALL=C comm -12 "$_ps_a" "$_ps_b")" ]; then
        _ps_skipped=1
        printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "файлы пересекаются" >>"$PLAN"
      else
        printf 'mv\t%s\t%s\t0\t%s\n' "$_ps_cat" "$_ps_stub" "$_ps_rep" >>"$PLAN"
      fi
    else
      printf '%s\n' "$_ps_stub" | _tl_emit_paths rm "$_ps_cat" "нет одноимённого отчёта" >>"$PLAN"
    fi
  done <"$_ps_names"
  rm -f "$_ps_a" "$_ps_b" "$_ps_names"
  # остаток frontend/build — после обрывков
  if [ "$_ps_skipped" = 1 ]; then
    printf 'skip\t%s\t%s\t0\t%s\n' "$_ps_cat" "$ROOT/frontend/build" "остались пропущенные обрывки" >>"$PLAN"
  else
    # sweep убирает только пустые каталоги: если перенос обрывка не удался, его файлы не пропадут
    printf 'sweep\t%s\t%s\t0\t%s\n' "$_ps_cat" "$ROOT/frontend/build" "остаток после обрывков" >>"$PLAN"
  fi
}

# Корневые хвосты (D-21): сборки тестов и покрытия, кэши, выводы Playwright. Удаляется только то,
# что git не отслеживает.
plan_tails() {
  _pt_cat="корневые хвосты"
  _pt_list=$(mktemp)
  {
    [ -f "$ROOT/xcp" ] && [ ! -L "$ROOT/xcp" ] && printf '%s\n' "$ROOT/xcp"
    for _pt_f in "$ROOT"/*.test "$ROOT"/*.out; do
      if [ -f "$_pt_f" ] && [ ! -L "$_pt_f" ]; then
        printf '%s\n' "$_pt_f"
      fi
    done
    for _pt_f in node_modules commands.md .go frontend/playwright-report frontend/test-results \
      frontend/test-results-router frontend/test-results-router.json build/xcp; do
      if [ -e "$ROOT/$_pt_f" ] || [ -L "$ROOT/$_pt_f" ]; then
        printf '%s\n' "$ROOT/$_pt_f"
      fi
    done
    for _pt_f in "$ROOT"/build/xcp_test_* "$ROOT"/build/*.log; do
      if [ -e "$_pt_f" ] || [ -L "$_pt_f" ]; then
        printf '%s\n' "$_pt_f"
      fi
    done
  } >"$_pt_list" || true
  while IFS= read -r _pt_p; do
    _pt_rel=${_pt_p#"$ROOT"/}
    if [ -n "$(git ls-files -- "$_pt_rel")" ]; then
      printf 'skip\t%s\t%s\t0\t%s\n' "$_pt_cat" "$_pt_p" "отслеживается git" >>"$PLAN"
    else
      printf '%s\n' "$_pt_p" | _tl_emit_paths rm "$_pt_cat" "" >>"$PLAN"
    fi
  done <"$_pt_list"
  rm -f "$_pt_list"
}

# --- git: worktree ---------------------------------------------------------------------
# Правила — в шапке. Все вызовы git по чужому worktree идут с GIT_OPTIONAL_LOCKS=0: опрос
# состояния не должен обновлять их индекс (меняется mtime, ломается оценка возраста).

export GIT_OPTIONAL_LOCKS=0

MAIN=$(rt_main_root)
GIT_COMMON=$(rt_common_dir)
# вершина и ветка основной копии читаются из общего каталога git: так не нужен git -C на чужой каталог
MAIN_HEAD=$(git --git-dir="$GIT_COMMON" rev-parse HEAD 2>/dev/null || true)
MAIN_BRANCH=$(git --git-dir="$GIT_COMMON" symbolic-ref --short -q HEAD 2>/dev/null || true)
WT_HOURS=${XCP_TIDY_WORKTREE_HOURS:-24}
case "$WT_HOURS" in
  '' | *[!0-9]*)
    echo "tidy: XCP_TIDY_WORKTREE_HOURS должна быть числом: $WT_HOURS" >&2
    exit 2
    ;;
esac

# Ветка origin защищена маской XCP_TIDY_PROTECT_REMOTE: tidy_protected_remote <имя без origin/>
tidy_protected_remote() {
  [ -n "${XCP_TIDY_PROTECT_REMOTE:-}" ] || return 1
  set -f
  for _pr_pat in $(printf '%s' "$XCP_TIDY_PROTECT_REMOTE" | tr ',' ' '); do
    # shellcheck disable=SC2254
    case "$1" in $_pr_pat)
      set +f
      return 0
      ;;
    esac
  done
  set +f
  return 1
}

# Строка списка (по одной на строку) входит в список: tidy_in_list <строка> <список>
tidy_in_list() {
  printf '%s\n' "$2" | grep -Fxq -- "$1"
}

pl_skip() { # <категория> <путь> <причина>
  printf 'skip\t%s\t%s\t0\t%s\n' "$1" "$2" "$3" >>"$PLAN"
}

# Слита ли вершина (D-18): из origin/main или (кроме detached) из текущей ветки основной копии.
wt_is_merged() { # <sha> <detached 0/1>
  git merge-base --is-ancestor "$1" origin/main 2>/dev/null && return 0
  [ "$2" = 1 ] && return 1
  [ -n "$MAIN_HEAD" ] || return 1
  git merge-base --is-ancestor "$1" "$MAIN_HEAD" 2>/dev/null
}

# Перечень worktree в $WT_LIST: путь, HEAD, ветка (или -), detached, locked, prunable — через TAB.
wt_collect() {
  git worktree list --porcelain | awk '
    function flush() {
      if (p != "") print p "\t" (h == "" ? "-" : h) "\t" (b == "" ? "-" : b) "\t" d "\t" l "\t" pr
      p = ""; h = ""; b = ""; d = 0; l = 0; pr = 0
    }
    /^worktree / { flush(); p = substr($0, 10); next }
    /^HEAD / { h = substr($0, 6); next }
    /^branch / { b = substr($0, 8); sub(/^refs\/heads\//, "", b); next }
    /^detached/ { d = 1; next }
    /^locked/ { l = 1; next }
    /^prunable/ { pr = 1; next }
    END { flush() }' >"$WT_LIST"
}

# Изменения worktree без frontend/node_modules → $WT_TMP (по пути на строку). Код 1 — статус не прочитан.
wt_changes() { # <worktree>
  _wc_raw=$(mktemp)
  if ! git -C "$1" status --porcelain=v1 -z --untracked-files=all >"$_wc_raw" 2>/dev/null; then
    rm -f "$_wc_raw"
    return 1
  fi
  tr '\0' '\n' <"$_wc_raw" | awk '
    skip { skip = 0; next }
    {
      x = substr($0, 1, 1); y = substr($0, 2, 1); p = substr($0, 4)
      if (x == "R" || x == "C" || y == "R" || y == "C") skip = 1
      if (p == "frontend/node_modules" || index(p, "frontend/node_modules/") == 1) next
      print p
    }' >"$WT_TMP"
  rm -f "$_wc_raw"
}

# Среди изменений $WT_TMP есть неотслеживаемый вложенный git-репозиторий (запись «каталог/» с
# собственным .git): git add -A сохранил бы в rescue-коммит лишь gitlink, а remove удалил бы
# содержимое. Код 0 — есть.
wt_nested_repo() { # <worktree>
  while IFS= read -r _wn_p; do
    case "$_wn_p" in
      */) [ ! -e "$1/${_wn_p}.git" ] || return 0 ;;
    esac
  done <"$WT_TMP"
  return 1
}

# Дети каталога worktree на один уровень (по файловой системе, ссылки не разыменовываются) в
# виде записей wt_ignored: каталог с файлами — «путь/», прочее — «путь»; пустые каталоги не
# считаются. Код 1 — каталог не прочитан.
wt_ign_children() { # <worktree> <каталог от корня worktree, без слеша на конце>
  _wk_f=$(mktemp)
  if ! (cd "$1" && find "$2" -mindepth 1 -maxdepth 1 >"$_wk_f" 2>/dev/null); then
    rm -f "$_wk_f"
    return 1
  fi
  while IFS= read -r _wk_c; do
    [ -n "$_wk_c" ] || continue
    if [ -d "$1/$_wk_c" ] && [ ! -L "$1/$_wk_c" ]; then
      if _wk_r=$(cd "$1/$_wk_c" && find . -mindepth 1 ! -type d -print -quit 2>/dev/null); then
        [ -n "$_wk_r" ] || continue
      fi
      printf '%s/\n' "$_wk_c"
    else
      printf '%s\n' "$_wk_c"
    fi
  done <"$_wk_f"
  rm -f "$_wk_f"
}

# Игнорируемое содержимое worktree, которое git worktree remove уничтожил бы вместе с каталогом
# (rescue-коммит его не берёт: .planning, отчёты build/router/<отчёт>, *.test, журналы…), за
# вычетом воспроизводимого → $WT_IGN (по записи на строку, пути от корня worktree; каталог —
# со слешем на конце). Воспроизводимое (закрытый список): frontend/node_modules, frontend/dist/,
# .go/, .go-cache/, build/xcp*, build/router/bin/, build/router/release/ и любая символическая
# ссылка (remove снимает саму ссылку, не идя по ней). Каталог build/ и build/router/ git отдаёт
# одной записью — раскрывается на один уровень по файловой системе. Код 1 — перечень не получен
# (вызывающий считает это отказом). Перевод строки в имени даёт лишнюю запись: N только растёт.
wt_ignored() { # <worktree>
  : >"$WT_IGN"
  _wi_raw=$(mktemp)
  _wi_lines=$(mktemp)
  _wi_cand=$(mktemp)
  _wi_kids=$(mktemp)
  _wi_rc=0
  # Без --no-empty-directory: с ним git не показывает игнорируемые файлы внутри неотслеживаемого
  # неигнорируемого каталога (новый пакет с .env рядом с кодом). Пустые каталоги отсеиваются ниже.
  if git -C "$1" ls-files --others --ignored --exclude-standard --directory -z >"$_wi_raw" 2>/dev/null; then
    tr '\0' '\n' <"$_wi_raw" | LC_ALL=C sort -u >"$_wi_lines"
    while IFS= read -r _wi_e; do
      [ -n "$_wi_e" ] || continue
      case "$_wi_e" in
        build/ | build/router/)
          wt_ign_children "$1" "${_wi_e%/}" >"$_wi_kids" || {
            _wi_rc=1
            break
          }
          while IFS= read -r _wi_k; do
            if [ "$_wi_k" = "build/router/" ]; then
              wt_ign_children "$1" build/router >>"$_wi_cand" || {
                _wi_rc=1
                break
              }
            else
              printf '%s\n' "$_wi_k" >>"$_wi_cand"
            fi
          done <"$_wi_kids"
          [ "$_wi_rc" = 0 ] || break
          ;;
        */)
          # пустой игнорируемый каталог при снятии ничего не теряет; ошибка чтения — считаем непустым
          _wi_f=$(find "$1/${_wi_e%/}" -mindepth 1 ! -type d -print -quit 2>/dev/null) || _wi_f=x
          [ -n "$_wi_f" ] || continue
          printf '%s\n' "$_wi_e" >>"$_wi_cand"
          ;;
        *) printf '%s\n' "$_wi_e" >>"$_wi_cand" ;;
      esac
    done <"$_wi_lines"
  else
    _wi_rc=1
  fi
  if [ "$_wi_rc" = 0 ]; then
    while IFS= read -r _wi_e; do
      [ -n "$_wi_e" ] || continue
      case "$_wi_e" in
        frontend/node_modules | frontend/node_modules/* | frontend/dist/* | .go/* | .go-cache/* | build/xcp* | build/router/bin/* | build/router/release/*) continue ;;
      esac
      if [ -L "$1/${_wi_e%/}" ]; then continue; fi
      printf '%s\n' "$_wi_e" >>"$WT_IGN"
    done <"$_wi_cand"
  else
    : >"$WT_IGN"
  fi
  rm -f "$_wi_raw" "$_wi_lines" "$_wi_cand" "$_wi_kids"
  return "$_wi_rc"
}

# Примечание плана об игнорируемом из $WT_IGN: «игнорируемые файлы: N (п1, …, п5, … и ещё K)».
# Из путей убираются управляющие символы (TAB — разделитель полей записи плана).
wt_ign_note() {
  _wn_n=$(wc -l <"$WT_IGN" | tr -d ' ')
  _wn_l=$(head -n 5 "$WT_IGN" | tr -d '\000-\011\013-\037\177' | awk '{ printf "%s%s", (NR > 1 ? ", " : ""), $0 }')
  if [ "$_wn_n" -gt 5 ]; then
    _wn_l="$_wn_l, … и ещё $((_wn_n - 5))"
  fi
  printf 'игнорируемые файлы: %s (%s)' "$_wn_n" "$_wn_l"
}

# Возраст правок: наибольший mtime существующих путей из $WT_TMP; пусто, если путей нет.
wt_age_mtime() { # <worktree>
  (cd "$1" && xargs -d '\n' stat -c %Y -- 2>/dev/null) <"$WT_TMP" | sort -n | tail -n 1
}

# Классификация одного worktree в план. Пропуски — по порядку правил D-18…D-20.
plan_wt_one() { # <путь> <HEAD> <ветка|-> <detached> <locked> <prunable> <разовое 0/1>
  _po_p=$1
  _po_h=$2
  _po_b=$3
  _po_d=$4
  _po_l=$5
  _po_pr=$6
  _po_f=$7
  _po_c="worktree"
  _po_one=""
  if [ "$_po_f" = 1 ]; then _po_one="разовое: --rescue"; fi
  if [ "$_po_pr" = 1 ]; then
    pl_skip "$_po_c" "$_po_p" "каталога нет — снимет git worktree prune"
    return 0
  fi
  case "$_po_p" in
    "$MAIN"/?*) ;;
    *)
      pl_skip "$_po_c" "$_po_p" "вне репозитория"
      return 0
      ;;
  esac
  if [ "$_po_p" = "$ROOT" ]; then
    pl_skip "$_po_c" "$_po_p" "текущий"
    return 0
  fi
  if [ "$_po_l" = 1 ]; then
    pl_skip "$_po_c" "$_po_p" "заблокирован"
    return 0
  fi
  if [ "$LOCK_BUSY" = 1 ]; then
    pl_skip "$_po_c" "$_po_p" "блокировка занята прогоном"
    return 0
  fi
  if [ ! -d "$_po_p" ]; then
    pl_skip "$_po_c" "$_po_p" "каталога нет — снимет git worktree prune"
    return 0
  fi
  if [ "$_po_d" = 0 ]; then
    case "$_po_b" in
      main)
        pl_skip "$_po_c" "$_po_p" "ветка main"
        return 0
        ;;
      rescue/*)
        pl_skip "$_po_c" "$_po_p" "ветка rescue/*"
        return 0
        ;;
    esac
  fi
  if [ "$_po_f" != 1 ] && ! wt_is_merged "$_po_h" "$_po_d"; then
    pl_skip "$_po_c" "$_po_p" "не слита"
    return 0
  fi
  if ! wt_changes "$_po_p"; then
    pl_skip "$_po_c" "$_po_p" "статус worktree не прочитан"
    return 0
  fi
  if wt_nested_repo "$_po_p"; then
    pl_skip "$_po_c" "$_po_p" "вложенный git-репозиторий"
    return 0
  fi
  _po_n=$(wc -l <"$WT_TMP" | tr -d ' ')
  if [ "$_po_f" != 1 ]; then
    _po_t=""
    if [ "$_po_n" -gt 0 ]; then
      _po_t=$(wt_age_mtime "$_po_p")
    else
      _po_gd=$(git -C "$_po_p" rev-parse --absolute-git-dir 2>/dev/null || true)
      if [ -n "$_po_gd" ]; then
        _po_t=$(stat -c %Y -- "$_po_gd/HEAD" 2>/dev/null || true)
      fi
    fi
    if [ -z "$_po_t" ]; then
      pl_skip "$_po_c" "$_po_p" "возраст не определён"
      return 0
    fi
    if [ $(($(date +%s) - _po_t)) -lt $((WT_HOURS * 3600)) ]; then
      pl_skip "$_po_c" "$_po_p" "правки моложе $WT_HOURS ч"
      return 0
    fi
  fi
  # игнорируемое вне воспроизводимого пропало бы при снятии (D-19): пропуск, ошибка — в безопасную сторону
  if ! wt_ignored "$_po_p"; then
    pl_skip "$_po_c" "$_po_p" "игнорируемые файлы не прочитаны"
    return 0
  fi
  if [ -s "$WT_IGN" ]; then
    _po_note=$(wt_ign_note)
    if [ -n "$_po_one" ]; then _po_note="$_po_one; $_po_note"; fi
    pl_skip "$_po_c" "$_po_p" "$_po_note"
    if [ "$_po_f" = 1 ]; then
      # разовое --rescue: владелец решает сам, поэтому полный список (D-23)
      _po_nm=$(basename "$_po_p")
      tr -d '\000-\011\013-\037\177' <"$WT_IGN" | while IFS= read -r _po_i; do
        echo "tidy: --rescue $_po_nm: игнорируемое: $_po_i" >&2
      done
      echo "tidy: --rescue $_po_nm: игнорируемые файлы не входят в rescue и пропали бы при снятии worktree — он пропущен; решение владельца: перенести нужное или удалить вручную и повторить" >&2
    fi
    return 0
  fi
  _po_kb=0
  if [ "${TIDY_SIZES:-0}" = 1 ]; then
    _po_kb=$(du -sk -- "$_po_p" 2>/dev/null | cut -f1)
  fi
  if [ "$_po_n" -gt 0 ]; then
    _po_note="файлов: $_po_n"
    if [ -n "$_po_one" ]; then _po_note="$_po_one; $_po_note"; fi
    printf 'wt-rescue\t%s\t%s\t%s\t%s\n' "$_po_c" "$_po_p" "${_po_kb:-0}" "$_po_note" >>"$PLAN"
  elif [ "$_po_f" = 1 ]; then
    printf 'wt-rescue\t%s\t%s\t%s\t%s\n' "$_po_c" "$_po_p" "${_po_kb:-0}" "$_po_one" >>"$PLAN"
  else
    printf 'wt-remove\t%s\t%s\t%s\t%s\n' "$_po_c" "$_po_p" "${_po_kb:-0}" "" >>"$PLAN"
  fi
}

plan_worktrees() {
  wt_collect
  _pw_named=""
  if [ -n "$RESCUE_LIST" ]; then
    while IFS= read -r _pw_n; do
      [ -n "$_pw_n" ] || continue
      _pw_hit=$(awk -F '\t' -v n="$_pw_n" -v m="$MAIN" '$1 != m { b = $1; sub(/.*\//, "", b); if ($1 == n || b == n) { print $1; exit } }' "$WT_LIST")
      if [ -z "$_pw_hit" ]; then
        echo "tidy: worktree не найден: $_pw_n" >&2
        exit 2
      fi
      _pw_named="$_pw_named$_pw_hit
"
    done <<EOF
$RESCUE_LIST
EOF
  fi
  while IFS="$_TL_TAB" read -r _pw_p _pw_h _pw_b _pw_d _pw_l _pw_pr; do
    [ "$_pw_p" != "$MAIN" ] || continue
    _pw_f=0
    if tidy_in_list "$_pw_p" "$_pw_named"; then _pw_f=1; fi
    plan_wt_one "$_pw_p" "$_pw_h" "$_pw_b" "$_pw_d" "$_pw_l" "$_pw_pr" "$_pw_f" </dev/null
  done <"$WT_LIST"
}

# Сведения о worktree из актуального перечня git: _wl_h _wl_b _wl_d _wl_l. Код 1 — такого нет.
wt_lookup() { # <путь>
  wt_collect
  _wl_line=$(awk -F '\t' -v p="$1" '$1 == p { print; exit }' "$WT_LIST")
  [ -n "$_wl_line" ] || return 1
  IFS="$_TL_TAB" read -r _wl_p _wl_h _wl_b _wl_d _wl_l _wl_pr <<EOF
$_wl_line
EOF
}

# Ветка worktree после его снятия: не main, не ветка основной копии, не rescue/*.
wt_drop_branch() { # <ветка|-> <detached>
  [ "$2" = 0 ] || return 0
  case "$1" in
    - | main | rescue/*) return 0 ;;
  esac
  [ "$1" != "$MAIN_BRANCH" ] || return 0
  git branch -D "$1" >/dev/null
}

# todo о спасённом (формат существующих todo); нет .planning/todos/pending — только строка в выводе.
wt_todo() { # <имя rescue> <имя worktree> <ветка|-> <detached> <HEAD> <коммит> <файл со списком путей>
  _wt_dir="$MAIN/.planning/todos/pending"
  if [ ! -d "$_wt_dir" ]; then
    echo "tidy: todo не записан (нет .planning/todos/pending): rescue/$1" >&2
    return 0
  fi
  _wt_f="$_wt_dir/rescue-$1.md"
  _wt_i=2
  while [ -e "$_wt_f" ]; do
    _wt_f="$_wt_dir/rescue-$1-$_wt_i.md"
    _wt_i=$((_wt_i + 1))
  done
  _wt_total=$(wc -l <"$7" | tr -d ' ')
  _wt_date=$(date -u +%Y-%m-%d)
  {
    printf '%s\n' '---'
    printf 'created: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)"
    printf 'title: "Разобрать rescue/%s: незакоммиченное из worktree %s"\n' "$1" "$2"
    printf '%s\n' 'area: tidy' 'severity: minor' 'priority: P3'
    if [ "$_wt_total" -gt 0 ]; then
      printf '%s\n' 'files:'
      head -n 30 "$7" | sed 's/\\/\\\\/g; s/"/\\"/g; s/^/  - "/; s/$/"/'
    else
      printf '%s\n' 'files: []'
    fi
    printf '%s\n\n' '---'
    printf '# Разобрать rescue/%s\n\n' "$1"
    printf '%s\n\n' "\`make tidy APPLY=1\` снял слитый worktree \`$2\` и сохранил его незакоммиченное в локальной ветке \`rescue/$1\`."
    if [ "$4" = 1 ] || [ "$3" = "-" ]; then
      printf '%s\n' '- исходный HEAD: отсоединён (detached)'
    else
      printf '%s\n' "- исходная ветка: \`$3\`"
    fi
    printf '%s\n' "- HEAD worktree: \`$5\`" "- rescue-коммит: \`$6\`" "- дата: $_wt_date" "- файлов в коммите: $_wt_total"
    printf '\n%s\n\n' '## Как разобрать'
    printf '%s\n' '```sh' "git show --stat rescue/$1" "git diff $5 rescue/$1" '```'
    if [ "$_wt_total" -gt 30 ]; then
      printf '\n%s\n' "В files — первые 30 путей, … и ещё $((_wt_total - 30)) (полный список: \`git show --stat rescue/$1\`)."
    fi
    printf '\n%s\n' 'Нужное перенести в работу или отбросить. Ветку `rescue/*` удаляет только владелец; на origin она не отправляется.'
  } >"$_wt_f"
  echo "tidy: todo записан: .planning/todos/pending/$(basename "$_wt_f")" >&2
}

# Спасение: WIP-коммит поверх HEAD worktree во временном индексе (индекс и файлы worktree не
# меняются, frontend/node_modules исключён) → ветка rescue/<имя> → todo → remove → ветка worktree.
# убрать временный индекс спасения и его каталог
_gr_clean() {
  rm -f "$_gr_idx"
  rmdir "$_gr_dir" 2>/dev/null || true
}

git_wt_rescue() { # <путь worktree>
  _gr_wt=$1
  if ! wt_lookup "$_gr_wt"; then
    echo "tidy: worktree не найден при выполнении: $(basename "$_gr_wt")" >&2
    return 1
  fi
  case "$_gr_wt" in
    "$MAIN"/?*) ;;
    *) return 1 ;;
  esac
  if [ "$_gr_wt" = "$ROOT" ] || [ "$_wl_l" = 1 ] || [ ! -d "$_gr_wt" ]; then
    echo "tidy: отказ спасать: $(basename "$_gr_wt") — текущий, заблокирован или каталога нет" >&2
    return 1
  fi
  _gr_name=$(basename "$_gr_wt")
  # игнорируемое rescue-коммит не берёт, а remove его удалил бы: отказ до ветки и todo (D-12, D-19)
  wt_ignored "$_gr_wt" || { echo "tidy: отказ спасать $_gr_name: игнорируемые файлы не прочитаны" >&2; return 1; }
  [ ! -s "$WT_IGN" ] || { echo "tidy: отказ спасать $_gr_name: игнорируемые файлы: $(wc -l <"$WT_IGN" | tr -d ' ')" >&2; return 1; }
  _gr_head=$_wl_h
  _gr_br=$_wl_b
  _gr_det=$_wl_d
  _gr_rb=$_gr_name
  _gr_i=2
  while git show-ref --verify --quiet "refs/heads/rescue/$_gr_rb"; do
    _gr_rb="$_gr_name-$_gr_i"
    _gr_i=$((_gr_i + 1))
  done
  if ! git check-ref-format --branch "rescue/$_gr_rb" >/dev/null 2>&1; then
    echo "tidy: недопустимое имя ветки rescue/$_gr_rb" >&2
    return 1
  fi
  # временный индекс в собственном каталоге: mktemp -u оставил бы окно подмены имени в общем /tmp
  _gr_dir=$(mktemp -d) || return 1
  _gr_idx=$_gr_dir/index
  if ! GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" read-tree "$_gr_head"; then
    _gr_clean
    return 1
  fi
  # Ссылка frontend/node_modules (путь ПК) в rescue не попадает. Под актуальным .gitignore она
  # игнорируется и обычный add её не берёт (exclude-путь, попавший под .gitignore, git считает
  # ошибкой и выходит с кодом 1, хотя всё остальное добавляет); под старым .gitignore исключаем явно.
  if git -C "$_gr_wt" check-ignore -q frontend/node_modules 2>/dev/null; then
    GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" add -A -- . || {
      _gr_clean
      return 1
    }
  else
    GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" add -A -- . ':(exclude)frontend/node_modules' || {
      _gr_clean
      return 1
    }
  fi
  GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" rm -r --cached -q --ignore-unmatch -- frontend/node_modules 2>/dev/null || true
  # вложенный git-репозиторий add превращает в gitlink без файлов, а remove удалил бы его содержимое
  if GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" diff --cached --raw --no-renames "$_gr_head" | awk '$2 == "160000" { f = 1 } END { exit !f }'; then
    _gr_clean
    echo "tidy: отказ спасать $_gr_name: вложенный git-репозиторий (gitlink) не сохраняется" >&2
    return 1
  fi
  _gr_tree=$(GIT_INDEX_FILE=$_gr_idx git -C "$_gr_wt" write-tree) || {
    _gr_clean
    return 1
  }
  _gr_clean
  if [ "$_gr_tree" = "$(git rev-parse "$_gr_head^{tree}")" ]; then
    _gr_commit=$_gr_head
  else
    _gr_commit=$(git commit-tree "$_gr_tree" -p "$_gr_head" \
      -m "chore(rescue): незакоммиченное из worktree $_gr_name ($(date -u +%Y-%m-%d))" \
      -m "Служебный коммит make tidy: все изменённые, удалённые и неотслеживаемые файлы worktree, кроме frontend/node_modules.") || return 1
  fi
  git update-ref "refs/heads/rescue/$_gr_rb" "$_gr_commit" "" || return 1
  _gr_files=$(mktemp)
  git diff --name-only --no-renames -z "$_gr_head" "$_gr_commit" | tr '\0' '\n' >"$_gr_files"
  wt_todo "$_gr_rb" "$_gr_name" "$_gr_br" "$_gr_det" "$_gr_head" "$_gr_commit" "$_gr_files" || true
  rm -f "$_gr_files"
  echo "tidy: rescue/$_gr_rb -> $(git rev-parse --short "$_gr_commit") (worktree $_gr_name)" >&2
  wt_ignored "$_gr_wt" || { echo "tidy: worktree $_gr_name не снят: игнорируемые файлы не прочитаны; rescue/$_gr_rb и todo сохранены" >&2; return 1; }
  [ ! -s "$WT_IGN" ] || { echo "tidy: worktree $_gr_name не снят: появились игнорируемые файлы: $(wc -l <"$WT_IGN" | tr -d ' '); rescue/$_gr_rb и todo сохранены" >&2; return 1; }
  git worktree remove --force "$_gr_wt" || return 1
  wt_drop_branch "$_gr_br" "$_gr_det"
}

# Снятие чистого слитого worktree и его ветки (с повторной проверкой).
git_wt_remove() { # <путь worktree>
  _gm_wt=$1
  wt_lookup "$_gm_wt" || return 1
  case "$_gm_wt" in
    "$MAIN"/?*) ;;
    *) return 1 ;;
  esac
  if [ "$_gm_wt" = "$ROOT" ] || [ "$_wl_l" = 1 ] || [ ! -d "$_gm_wt" ]; then
    return 1
  fi
  wt_is_merged "$_wl_h" "$_wl_d" || return 1
  wt_changes "$_gm_wt" || return 1
  [ ! -s "$WT_TMP" ] || return 1
  _gm_br=$_wl_b
  _gm_det=$_wl_d
  _gm_name=$(basename "$_gm_wt")
  wt_ignored "$_gm_wt" || { echo "tidy: worktree $_gm_name не снят: игнорируемые файлы не прочитаны" >&2; return 1; }
  [ ! -s "$WT_IGN" ] || { echo "tidy: worktree $_gm_name не снят: игнорируемые файлы: $(wc -l <"$WT_IGN" | tr -d ' ')" >&2; return 1; }
  if ! git worktree remove "$_gm_wt" 2>/dev/null; then
    # отказ допустим только из-за неотслеживаемой ссылки frontend/node_modules (старый .gitignore)
    wt_changes "$_gm_wt" || return 1
    [ ! -s "$WT_TMP" ] || return 1
    wt_ignored "$_gm_wt" || { echo "tidy: worktree $_gm_name не снят: игнорируемые файлы не прочитаны" >&2; return 1; }
    [ ! -s "$WT_IGN" ] || { echo "tidy: worktree $_gm_name не снят: игнорируемые файлы: $(wc -l <"$WT_IGN" | tr -d ' ')" >&2; return 1; }
    git worktree remove --force "$_gm_wt" || return 1
  fi
  wt_drop_branch "$_gm_br" "$_gm_det"
}

# --- git: prune, ветки, origin, stash, gc -----------------------------------------------

# Записи о worktree с пропавшим каталогом: предпросмотр git worktree prune.
plan_prune() {
  git worktree prune --dry-run -v 2>&1 </dev/null |
    awk 'NF { printf "prune\tgit\t%s\t0\t\n", $0 }' >>"$PLAN"
}

# Локальные ветки (D-18, D-21, D-29). Разовый --force-branch снимает проверку слитости с названных.
plan_branches() {
  _pb_c="локальные ветки"
  wt_collect
  _pb_occ=$(awk -F '\t' '$3 != "-" { print $3 }' "$WT_LIST")
  if [ -n "$FORCE_BRANCHES" ]; then
    while IFS= read -r _pb_n; do
      [ -n "$_pb_n" ] || continue
      if ! git check-ref-format --branch "$_pb_n" >/dev/null 2>&1; then
        echo "tidy: --force-branch: недопустимое имя ветки: $_pb_n" >&2
        exit 2
      fi
      if ! git show-ref --verify --quiet "refs/heads/$_pb_n"; then
        echo "tidy: --force-branch: ветки нет: $_pb_n" >&2
        exit 2
      fi
      case "$_pb_n" in
        main | rescue/*)
          echo "tidy: --force-branch: ветку не удаляем: $_pb_n" >&2
          exit 2
          ;;
      esac
      if [ "$_pb_n" = "$MAIN_BRANCH" ] || tidy_in_list "$_pb_n" "$_pb_occ"; then
        echo "tidy: --force-branch: ветка текущая или занята worktree: $_pb_n" >&2
        exit 2
      fi
    done <<EOF
$FORCE_BRANCHES
EOF
  fi
  _pb_list=$(mktemp)
  git for-each-ref refs/heads --format='%(refname:short)' >"$_pb_list"
  _pb_rescue=0
  while IFS= read -r _pb_b; do
    case "$_pb_b" in
      main) continue ;;
      rescue/*)
        _pb_rescue=$((_pb_rescue + 1))
        continue
        ;;
    esac
    if [ "$_pb_b" = "$MAIN_BRANCH" ] || tidy_in_list "$_pb_b" "$_pb_occ"; then
      pl_skip "$_pb_c" "$_pb_b" "занята worktree"
      continue
    fi
    _pb_sha=$(git rev-parse "refs/heads/$_pb_b")
    if tidy_in_list "$_pb_b" "$FORCE_BRANCHES"; then
      if wt_is_merged "$_pb_sha" 0; then
        _pb_note="разовое: слита, вершина $_pb_sha"
      else
        _pb_cnt=$(git rev-list --count "origin/main..$_pb_b" 2>/dev/null || echo "?")
        _pb_note="разовое: не слита, коммитов вне origin/main: $_pb_cnt, вершина $_pb_sha"
      fi
      printf 'br-delete\t%s\t%s\t0\t%s\n' "$_pb_c" "$_pb_b" "$_pb_note" >>"$PLAN"
    elif wt_is_merged "$_pb_sha" 0; then
      printf 'br-delete\t%s\t%s\t0\t%s\n' "$_pb_c" "$_pb_b" "" >>"$PLAN"
    else
      pl_skip "$_pb_c" "$_pb_b" "не слита"
    fi
  done <"$_pb_list"
  rm -f "$_pb_list"
  if [ "$_pb_rescue" -gt 0 ]; then
    pl_skip "$_pb_c" "rescue/*: $_pb_rescue" "удаляет только владелец"
  fi
}

# Одна ветка origin: слитость — только по origin/main (строже D-18, ошибка в безопасную сторону).
plan_origin_one() { # <имя без origin/>
  _po1_c="ветки origin"
  case "$1" in
    main)
      pl_skip "$_po1_c" "$1" "main не удаляется"
      return 0
      ;;
    rescue/*)
      pl_skip "$_po1_c" "$1" "rescue/*"
      return 0
      ;;
  esac
  if tidy_protected_remote "$1"; then
    pl_skip "$_po1_c" "$1" "защищена (XCP_TIDY_PROTECT_REMOTE)"
    return 0
  fi
  if git merge-base --is-ancestor "refs/remotes/origin/$1" origin/main 2>/dev/null; then
    printf 'origin-delete\t%s\t%s\t0\t%s\n' "$_po1_c" "$1" "" >>"$PLAN"
  else
    pl_skip "$_po1_c" "$1" "не слита"
  fi
}

# Ветки origin — только с --remote: сначала git fetch --prune. Без связи — один пропуск.
plan_origin() {
  _po_c="ветки origin"
  # fd 9 (блокировка стенда) потомкам не отдаётся: фоновое git maintenance после fetch удержало бы её
  if ! git fetch --prune origin </dev/null >/dev/null 2>&1 9>&-; then
    pl_skip "$_po_c" "origin" "нет связи с origin"
    return 0
  fi
  _po_all=$(git for-each-ref refs/remotes/origin --format='%(refname:short)' | grep -vxE 'origin|origin/HEAD|origin/main' | sed 's|^origin/||' || true)
  if [ -n "$REMOTE_BRANCHES" ]; then
    while IFS= read -r _po_n; do
      [ -n "$_po_n" ] || continue
      if [ "$_po_n" = main ] || tidy_in_list "$_po_n" "$_po_all"; then
        plan_origin_one "$_po_n"
      else
        pl_skip "$_po_c" "$_po_n" "нет на origin"
      fi
    done <<EOF
$REMOTE_BRANCHES
EOF
  else
    while IFS= read -r _po_n; do
      [ -n "$_po_n" ] || continue
      plan_origin_one "$_po_n"
    done <<EOF
$_po_all
EOF
  fi
}

# stash только показывается (пропуск «stash», в пути — начало sha коммита); названные
# --drop-stash stash@{N}=sha — удаление, по убыванию номера, только если коммит записи начинается с sha.
plan_stash() {
  _ps2_c="stash"
  _ps2_refs=""
  if [ -n "$DROP_STASHES" ]; then
    while IFS= read -r _ps2_n; do
      [ -n "$_ps2_n" ] || continue
      _ps2_ref=${_ps2_n%%=*}
      _ps2_want=${_ps2_n#*=}
      _ps2_cur=$(git rev-parse --verify --quiet "$_ps2_ref" 2>/dev/null || true)
      if [ -z "$_ps2_cur" ]; then
        echo "tidy: stash не найден: $_ps2_ref" >&2
        exit 2
      fi
      case "$_ps2_cur" in
        "$_ps2_want"*) ;;
        *)
          echo "tidy: stash $_ps2_ref теперь другой (коммит $_ps2_cur, ожидалось $_ps2_want): номера сдвигаются, сверьтесь с показом" >&2
          exit 2
          ;;
      esac
      _ps2_refs="$_ps2_refs$_ps2_ref
"
    done <<EOF
$DROP_STASHES
EOF
  fi
  _ps2_list=$(mktemp)
  _ps2_drop=$(mktemp)
  git stash list --format='%gd%x09%H%x09%gs' >"$_ps2_list"
  while IFS="$_TL_TAB" read -r _ps2_r _ps2_h _ps2_s; do
    [ -n "$_ps2_r" ] || continue
    if tidy_in_list "$_ps2_r" "$_ps2_refs"; then
      printf '%s\t%s\t%s\t%s\n' "$(printf '%s' "$_ps2_r" | tr -dc '0-9')" "$_ps2_r" "$_ps2_h" "$_ps2_s" >>"$_ps2_drop"
    else
      pl_skip "$_ps2_c" "$_ps2_r ($(printf '%.7s' "$_ps2_h")): $_ps2_s" "stash"
    fi
  done <"$_ps2_list"
  sort -rn "$_ps2_drop" | while IFS="$_TL_TAB" read -r _ps2_i _ps2_r _ps2_h _ps2_s; do
    printf 'stash-drop\t%s\t%s\t0\tразовое: %s: %s\n' "$_ps2_c" "$_ps2_r" "$_ps2_h" "$_ps2_s"
  done >>"$PLAN"
  rm -f "$_ps2_list" "$_ps2_drop"
}

# gc — последним действием плана: обычный, без --prune=now (паки — история). С разовыми
# удалениями (--force-branch, --drop-stash) не выполняется: удалённое держится только недостижимыми
# объектами, которые gc убрал бы, если они старше gc.pruneExpire.
plan_gc() {
  if [ -n "$FORCE_BRANCHES" ] || [ -n "$DROP_STASHES" ]; then
    pl_skip "git" "git gc" "не выполняется вместе с --force-branch и --drop-stash"
    return 0
  fi
  _pg_info=$(git count-objects -v 2>/dev/null | awk -F ': ' '$1 == "size-pack" { sp = $2 } $1 == "count" { c = $2 } END { printf "size-pack %s КиБ, рыхлых объектов %s", sp, c }')
  printf 'gc\tgit\tgit gc\t0\t%s\n' "$_pg_info" >>"$PLAN"
}

# Строка-пояснение под заголовком категории показа.
category_info() { # <категория>
  case "$1" in
    "локальные ветки")
      _ci_age=$(git log -1 --format=%cr origin/main 2>/dev/null || true)
      if [ -n "$_ci_age" ]; then
        printf 'origin/main обновлён %s; свежий список веток origin — make tidy REMOTE=1\n' "$_ci_age"
      else
        printf 'origin/main не найден; все ветки считаются неслитыми\n'
      fi
      ;;
  esac
}

# Исполнение git-действий (слитость и принадлежность повторно проверяются перед удалением).
git_br_delete() { # <ветка> <примечание>
  git show-ref --verify --quiet "refs/heads/$1" || return 1
  case "$1" in
    main | rescue/*) return 1 ;;
  esac
  [ "$1" != "$MAIN_BRANCH" ] || return 1
  wt_collect
  if awk -F '\t' -v b="$1" '$3 == b { f = 1 } END { exit !f }' "$WT_LIST"; then
    echo "tidy: ветка занята worktree, пропуск: $1" >&2
    return 1
  fi
  case "$2" in
    "разовое"*) ;;
    *) wt_is_merged "$(git rev-parse "refs/heads/$1")" 0 || return 1 ;;
  esac
  case "$2" in
    "разовое"*) echo "tidy: удаляется ветка $1, вершина $(git rev-parse "refs/heads/$1") (вернуть: git branch $1 <sha>)" >&2 ;;
  esac
  git branch -D "$1" >/dev/null
}

git_origin_delete() { # <ветка без origin/>
  case "$1" in
    '' | main | rescue/*) return 1 ;;
  esac
  git check-ref-format --branch "$1" >/dev/null 2>&1 || return 1
  ! tidy_protected_remote "$1" || return 1
  _go_sha=$(git rev-parse --verify --quiet "refs/remotes/origin/$1") || return 1
  git merge-base --is-ancestor "$_go_sha" origin/main 2>/dev/null || return 1
  # удаление только если вершина на origin всё ещё та, что проверена на слитость
  git push --force-with-lease="refs/heads/$1:$_go_sha" origin ":refs/heads/$1"
}

git_stash_drop() { # <stash@{N}> <разовое: sha: тема>
  _gs_want=${2#разовое: }
  _gs_want=${_gs_want%%:*}
  _gs_cur=$(git rev-parse --verify --quiet "$1" 2>/dev/null || true)
  if [ -z "$_gs_cur" ] || [ "$_gs_cur" != "$_gs_want" ]; then
    echo "tidy: stash $1 изменился с момента построения плана, пропуск" >&2
    return 1
  fi
  echo "tidy: удаляется stash $1, коммит $_gs_cur (вернуть: git stash store -m <тема> $_gs_cur)" >&2
  git stash drop "$1" >/dev/null
}

# Перенос обрывка в отчёт (действие mv плана): tidy_exec отдаёт его сюда.
tidy_exec_hook() { # <действие> <категория> <путь> <КиБ> <примечание>
  case "$1" in
    mv)
      case "$5" in
        "$ROOT"/build/router/?*) ;;
        *) return 1 ;;
      esac
      [ -d "$5" ] || return 1
      # перед копированием файл отчёта не должен существовать (cp без -n затёр бы его)
      _mv_clash=$(cd "$3" && find . -type f | while IFS= read -r _mv_f; do
        if [ -e "$5/$_mv_f" ] || [ -L "$5/$_mv_f" ]; then
          printf '%s\n' "$_mv_f"
          break
        fi
      done)
      if [ -n "$_mv_clash" ]; then
        echo "tidy: перенос обрывка отменён, файл уже есть в отчёте: $_mv_clash" >&2
        return 1
      fi
      cp -Rp "$3/." "$5/" || return 1
      tidy_safe_rm "$ROOT" "$3"
      ;;
    sweep)
      [ "$3" = "$ROOT/frontend/build" ] || return 1
      [ -d "$3" ] && [ ! -L "$3" ] || return 0
      find "$3" -depth -type d -empty -delete
      ;;
    wt-rescue) git_wt_rescue "$3" </dev/null ;;
    wt-remove) git_wt_remove "$3" </dev/null ;;
    prune)
      # записей может быть несколько, команда одна: выполняется при первой
      if [ "${_TIDY_PRUNED:-0}" != 1 ]; then
        _TIDY_PRUNED=1
        git worktree prune -v </dev/null
      fi
      ;;
    br-delete) git_br_delete "$3" "$5" </dev/null ;;
    origin-delete) git_origin_delete "$3" </dev/null ;;
    stash-drop) git_stash_drop "$3" "$5" </dev/null ;;
    gc) git gc --quiet </dev/null 9>&- ;;
    *) return 1 ;;
  esac
}

# --- показ ---------------------------------------------------------------------------

show_category() {
  awk -F '\t' -v cat="$1" -v root="$ROOT/" -v root2="$MAIN/" -v info="$(category_info "$1")" '
    function human(kb) {
      if (kb >= 1024) return sprintf("%.1f МБ", kb / 1024)
      return sprintf("%d КиБ", kb)
    }
    BEGIN {
      word["rm"] = "удалить"; word["thin"] = "утончить"; word["mv"] = "перенести"; word["sweep"] = "убрать пустые"
      word["wt-remove"] = "снять worktree"; word["wt-rescue"] = "спасти"
      word["br-delete"] = "удалить ветку"; word["origin-delete"] = "удалить на origin"; word["stash-drop"] = "удалить stash"
      print "== " cat " =="
      if (info != "") print info
    }
    $2 == cat {
      p = $3
      if (index(p, root) == 1) p = substr(p, length(root) + 1)
      else if (index(p, root2) == 1) p = substr(p, length(root2) + 1)
      if ($1 == "skip") { printf "пропуск  %s  — %s\n", p, $5; sk++; next }
      w = ($1 in word) ? word[$1] : $1
      line = w "  " p
      if ($4 > 0 || $1 == "rm" || $1 == "thin" || $1 == "mv" || $1 == "sweep") line = line "  " human($4)
      nt = $5
      if (index(nt, root) == 1) nt = "-> " substr(nt, length(root) + 1)
      if (nt != "") line = line "  (" nt ")"
      print line
      n++; kb += $4
    }
    END { printf "-- %s: действий %d, пропусков %d, ≈ %.1f МБ\n", cat, n, sk, kb / 1024 }' "$PLAN"
}

show_plan() {
  printf '%s\n' "$CATEGORIES" | tr '|' '\n' | while IFS= read -r _cat; do
    [ -n "$_cat" ] || continue
    show_category "$_cat"
  done
  awk -F '\t' '$1 != "skip" { n++; kb += $4 } END { printf "итого: действий %d, ≈ %.1f МБ\n", n, kb / 1024 }' "$PLAN"
}

TIDY_SIZES=1
export TIDY_SIZES

# порядок записей = порядок исполнения: worktree, prune, ветки, origin, stash, файлы, gc
plan_worktrees
plan_prune
plan_branches
if [ "$REMOTE" = 1 ]; then
  plan_origin
fi
plan_stash
plan_builds
plan_reports
plan_releases
plan_legacy
plan_stubs
plan_tails
plan_gc

show_plan

if [ "$APPLY" != 1 ]; then
  echo "режим показа: ничего не удалено; удалить — make tidy APPLY=1"
  exit 0
fi

_rc=0
tidy_exec "$ROOT" <"$PLAN" || _rc=1
_freed=$(awk -F '\t' '$1 != "skip" { kb += $4 } END { printf "%.1f", kb / 1024 }' "$PLAN")
echo "освобождено ≈ $_freed МБ"
exit "$_rc"
