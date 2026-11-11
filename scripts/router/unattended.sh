#!/bin/sh
# scripts/router/unattended.sh — полный прогон без присмотра (make router-test-unattended; D-06, D-26…D-28).
#
# Инструмент без правил: когда запускать, решает владелец или оркестратор; напоминаний нет.
#
# Подкоманды:
#   start [--dry-run]  проверить входы и запустить отвязанный процесс прогона (setsid, nohup);
#                      команда возвращается сразу. --dry-run печатает строки ref=, sha=, release=,
#                      at=, worktree= и выходит, ничего не создавая
#   status             строка «состояние: нет|ожидание|очередь|идёт» (ожидание — до AT, очередь —
#                      run.sh ждёт блокировку стенда, идёт — прогон), затем ref=, sha=, release=, at=, лог=
#   run <каталог>      тело отвязанного процесса; вызывает только start
#   report <отчёт>     строки итога latest.md по отчёту (Итог, Причина остановки, Новые падения, Итог
#                      bisect, Не выполнено) в stdout, файлов не пишет; отчёт — каталог под build/router
#                      основной копии, иначе код 1
#
# Окружение (переменные make):
#   REF=<ref>           что проверять; по умолчанию origin/main после git fetch origin main
#   RELEASE=<тег|auto>  ставить ассет релиза (run.sh full с RELEASE): тег vX.Y.Z[-pre.N], существующий
#                       локально; auto — тег хвоста, покрывающего origin/main (release-guard.js tail-tag)
#   AT=<ЧЧ:ММ>          локальное время старта (24 ч); уже прошедшее сегодня — завтра
#
# Ход прогона: файл-признак alive, пульс раз в 60 с, удержание Windows и WSL -> ожидание AT ->
# заблокированный worktree build/unattended/wt-<короткий SHA> на SHA (--detach) -> run.sh full с
# RT_LOCK_WAIT=7200 (очередь на роутеры не освободилась за 2 ч — итог «пропущен: стенд занят») ->
# запись отчёта в историю (history.mjs append, если скрипт уже есть в основной копии) -> latest.md
# и unread -> снятие worktree (ловушка EXIT и сигналов). Каталог build/router worktree — ссылка на
# build/router основной копии: отчёт и ссылка last оказываются там. frontend/node_modules — ссылка на
# node_modules основной копии при совпадающем package-lock.json, иначе npm ci.
#
# Удержание (D-27): скрытый процесс PowerShell ставит SetThreadExecutionState(ES_CONTINUOUS |
# ES_SYSTEM_REQUIRED) и держит клиент wsl.exe, пока существует файл-признак; затем возвращает
# ES_CONTINUOUS. Текст сценария — константа, подставляются только проверенные имя дистрибутива и путь
# файла-признака. .wslconfig и постоянные настройки сна не трогаются. Нет powershell.exe — строка в
# логе «удержание WSL недоступно», прогон идёт.
#
# Каталог состояния — build/router/unattended основной копии (вне ротации make tidy, D-20):
#   state             pid, phase, ref, sha, release, at, started, run — пока идёт прогон
#   alive             файл-признак прогона
#   runs/<UTC>-<sha>/ log (вывод процесса), run.log (вывод run.sh), heartbeat.tsv (эпоха и время раз
#                     в 60 с), meta (ref, sha, release, at, created; в конце verdict, rc, report,
#                     finished), копии unattended.sh и lib.sh, которыми идёт прогон
#   latest.md         итог последнего прогона или пропуска (один экран): строка итога (из «- Итог:»
#                     summary.md, иначе счётчики passed/failed), «- Причина остановки: смена версии
#                     стенда — ожидалась <E>, на стенде <O>, граница <B> (<цель>, <ядро>)» по первой
#                     строке stop <отчёт>/<цель>/version-stop.tsv, новые падения (проверка и ядро, без
#                     детали), строки «- Итог bisect», непройденное (этапы с кодом 3 или 130, SKIP «нет
#                     связи», <цель>/notrun.txt, строки notrun version-stop.tsv), запись в историю,
#                     наибольший разрыв пульса
#   unread            одна строка для SessionStart: «полный прогон ГГГГ-ММ-ДД: зелёный|красный|пропущен,
#                     новых падений N» или «полный прогон ГГГГ-ММ-ДД: остановлен — смена версии стенда»;
#                     читает и снимает release-guard.js status --event SessionStart
#
# Вердикты meta: зелёный, красный, прерван (код 130), остановлен (строка stop в version-stop.tsv отчёта,
# D-13), пропущен, не начат. Прерванный и остановленный прогоны в историю не пишутся.
#
# Пропуск без новых слияний: тот же SHA и тот же RELEASE, что у прошлого завершённого прогона
# (verdict зелёный или красный; остановленный сменой версии прогон не считается), — latest.md
# «пропущен», unread не ставится, прогон не идёт.
# В latest.md и unread не попадают детали результатов, адреса и имена узлов.
#
# Коды выхода: 0 — запущен, пропущен или напечатано; 1 — вход отклонён или прогон уже идёт;
# 2 — неизвестная подкоманда.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

ROOT=$(rt_main_root)
UD="$ROOT"/build/router/unattended
WT_BASE="$ROOT"/build/unattended
NL='
'

ua_die() {
  echo "router-test-unattended: $*" >&2
  exit 1
}

# Значение целиком соответствует расширенному регулярному выражению (без переводов строки).
ua_match() {
  case "$2" in
    *"$NL"*) return 1 ;;
  esac
  printf '%s\n' "$2" | grep -Eq "$1"
}

REF_RE='^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$'
TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$'
AT_RE='^([01][0-9]|2[0-3]):[0-5][0-9]$'

# PID живого процесса прогона из state (процесс существует и это unattended.sh run); иначе код 1.
ua_alive_pid() {
  [ -f "$UD/state" ] || return 1
  _ap=$(rt_kv pid <"$UD/state" | tr -dc '0-9')
  [ -n "$_ap" ] || return 1
  kill -0 "$_ap" 2>/dev/null || return 1
  tr '\0' ' ' <"/proc/$_ap/cmdline" 2>/dev/null | grep -q 'unattended.sh run' || return 1
  printf '%s\n' "$_ap"
}

# meta последнего завершённого прогона (verdict зелёный или красный); нет такого — пусто.
ua_last_done() {
  [ -d "$UD/runs" ] || return 0
  for _ld in $(LC_ALL=C ls -1 "$UD/runs" | LC_ALL=C sort -r); do
    _lm="$UD/runs/$_ld/meta"
    [ -f "$_lm" ] || continue
    case "$(rt_kv verdict <"$_lm")" in
      зелёный | красный)
        printf '%s\n' "$_lm"
        return 0
        ;;
    esac
  done
}

# --- start --------------------------------------------------------------------------------

ua_start() {
  _dry=0
  if [ "${1:-}" = --dry-run ]; then
    _dry=1
  elif [ -n "${1:-}" ]; then
    ua_die "start: неизвестный ключ '$1' (есть только --dry-run)"
  fi

  _ref=${REF:-}
  if [ -z "$_ref" ]; then
    git fetch -q origin main || ua_die "git fetch origin main не удался"
    _ref=origin/main
  fi
  ua_match "$REF_RE" "$_ref" || ua_die "REF: недопустимая ссылка"
  _sha=$(git rev-parse --verify --quiet "$_ref^{commit}") || ua_die "REF: '$_ref' не указывает на коммит"
  _short=$(git rev-parse --short "$_sha")

  _rel=${RELEASE:-}
  if [ "$_rel" = auto ]; then
    _rel=$(node "$SCRIPT_DIR/../release-guard.js" tail-tag) || ua_die "RELEASE=auto: нет пре-релиза, покрывающего origin/main"
  fi
  if [ -n "$_rel" ]; then
    ua_match "$TAG_RE" "$_rel" || ua_die "RELEASE: недопустимый тег (ожидается vX.Y.Z или vX.Y.Z-beta.N)"
    if ! git rev-parse -q --verify "refs/tags/$_rel^{commit}" >/dev/null 2>&1 && [ "${RELEASE:-}" = auto ]; then
      git fetch -q origin "refs/tags/$_rel:refs/tags/$_rel" || true
    fi
    git rev-parse -q --verify "refs/tags/$_rel^{commit}" >/dev/null 2>&1 || ua_die "RELEASE: тега $_rel нет локально (git fetch --tags)"
  fi

  _at=${AT:-}
  if [ -n "$_at" ]; then
    ua_match "$AT_RE" "$_at" || ua_die "AT: ожидается ЧЧ:ММ (24 ч)"
  fi

  if [ "$_dry" = 1 ]; then
    echo "ref=$_ref"
    echo "sha=$_sha"
    echo "release=${_rel:--}"
    echo "at=${_at:--}"
    echo "worktree=$WT_BASE/wt-$_short"
    return 0
  fi

  # REF разрешён в рабочей копии вызывающего (REF=HEAD — его HEAD); дальше всё — от основной копии,
  # её же каталог текущий у отвязанного процесса
  cd "$ROOT"
  if _p=$(ua_alive_pid); then
    ua_die "прогон без присмотра уже идёт (pid $_p): sh scripts/router/unattended.sh status"
  fi

  # Пропуск без новых слияний: тот же SHA и RELEASE, что у прошлого завершённого прогона
  _last=$(ua_last_done)
  if [ -n "$_last" ] && [ "$(rt_kv sha <"$_last")" = "$_sha" ] && [ "$(rt_kv release <"$_last")" = "${_rel:--}" ]; then
    mkdir -p "$UD"
    {
      echo "# Полный прогон без присмотра"
      echo
      echo "- Дата: $(date '+%Y-%m-%d %H:%M')"
      echo "- ref: $_ref, sha: $_sha"
      echo "- Релиз: ${_rel:--}"
      echo "- Итог: пропущен: нет новых слияний с прошлого полного прогона ($(basename "$(dirname "$_last")"))"
    } >"$UD/latest.md"
    echo "router-test-unattended: пропущен — нет новых слияний с прошлого полного прогона"
    return 0
  fi

  _run="$UD/runs/$(date -u +%Y%m%d-%H%M%S)-$_short"
  mkdir -p "$_run"
  {
    echo "ref=$_ref"
    echo "sha=$_sha"
    echo "release=${_rel:--}"
    echo "at=${_at:--}"
    echo "created=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$_run/meta"
  # прогон идёт копией скриптов: worktree вызывающего может исчезнуть раньше, чем прогон закончится
  cp "$SCRIPT_DIR/unattended.sh" "$SCRIPT_DIR/lib.sh" "$_run/"
  setsid nohup sh "$_run/unattended.sh" run "$_run" </dev/null >>"$_run/log" 2>&1 &
  _pid=$!
  # state до первой записи самим процессом: повторный start сразу видит идущий прогон
  {
    echo "pid=$_pid"
    echo "phase=запуск"
    echo "ref=$_ref"
    echo "sha=$_sha"
    echo "release=${_rel:--}"
    echo "at=${_at:--}"
    echo "run=$_run"
  } >"$UD/state"
  echo "router-test-unattended: запущен"
  echo "pid=$_pid"
  echo "sha=$_sha"
  echo "release=${_rel:--}"
  echo "at=${_at:--}"
  echo "лог=$_run/log"
}

# --- run ----------------------------------------------------------------------------------

# state текущего прогона с фазой: ua_state <фаза>
ua_state() {
  {
    echo "pid=$$"
    echo "phase=$1"
    echo "ref=$U_REF"
    echo "sha=$U_SHA"
    echo "release=$U_REL"
    echo "at=$U_AT"
    echo "started=$U_STARTED"
    echo "run=$RUN"
  } >"$UD/state.tmp"
  mv -f "$UD/state.tmp" "$UD/state"
}

# Удержание Windows и WSL на время прогона (D-27). Код всегда 0: без удержания прогон идёт.
ua_hold() {
  if ! command -v powershell.exe >/dev/null 2>&1 || [ -z "${WSL_DISTRO_NAME:-}" ]; then
    echo "удержание WSL недоступно: нет powershell.exe или WSL_DISTRO_NAME"
    return 0
  fi
  if ! ua_match '^[A-Za-z0-9._-]+$' "$WSL_DISTRO_NAME"; then
    echo "удержание WSL недоступно: недопустимое имя дистрибутива"
    return 0
  fi
  if ! ua_match '^/[A-Za-z0-9._/-]+$' "$UD/alive"; then
    echo "удержание WSL недоступно: недопустимый путь файла-признака"
    return 0
  fi
  # 2147483649 = ES_CONTINUOUS | ES_SYSTEM_REQUIRED, 2147483648 = ES_CONTINUOUS
  _ps="\$s = '[DllImport(\"kernel32.dll\")] public static extern uint SetThreadExecutionState(uint f);'
\$k = Add-Type -MemberDefinition \$s -Name XcpRtPower -Namespace XcpRt -PassThru
[void]\$k::SetThreadExecutionState([uint32]2147483649)
& wsl.exe -d $WSL_DISTRO_NAME --exec sh -c 'while [ -e $UD/alive ]; do sleep 30; done'
[void]\$k::SetThreadExecutionState([uint32]2147483648)"
  _b64=$(printf '%s' "$_ps" | iconv -f UTF-8 -t UTF-16LE | base64 -w0) || _b64=""
  if [ -z "$_b64" ]; then
    echo "удержание WSL недоступно: не удалось закодировать сценарий"
    return 0
  fi
  if (
    [ ! -d /mnt/c ] || cd /mnt/c
    powershell.exe -NoProfile -NonInteractive -Command \
      "Start-Process -WindowStyle Hidden powershell.exe -ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','$_b64'"
  ) >/dev/null 2>&1; then
    echo "удержание WSL: включено (дистрибутив $WSL_DISTRO_NAME)"
  else
    echo "удержание WSL недоступно: запуск PowerShell не удался"
  fi
}

# Ожидание ближайшего наступления локального времени AT.
ua_wait_at() {
  [ "$U_AT" != - ] || return 0
  _wn=$(date +%s)
  _wt=$(date -d "today $U_AT" +%s)
  [ "$_wt" -gt "$_wn" ] || _wt=$(date -d "tomorrow $U_AT" +%s)
  echo "ожидание до $(date -d "@$_wt" '+%Y-%m-%d %H:%M')"
  while [ "$(date +%s)" -lt "$_wt" ]; do
    sleep 30
  done
}

# Снятие worktree прогона: сначала ссылки (в основную копию не уходит ничего), затем unlock и remove.
ua_wt_remove() {
  [ -n "${WT:-}" ] || return 0
  [ ! -L "$WT/build"/router ] || rm -f "$WT/build"/router
  [ ! -L "$WT/frontend/node_modules" ] || rm -f "$WT/frontend/node_modules"
  git worktree unlock "$WT" >/dev/null 2>&1 || true
  git worktree remove --force "$WT" >/dev/null 2>&1 || true
  git worktree prune >/dev/null 2>&1 || true
}

ua_cleanup() {
  trap '' INT TERM HUP
  rm -f "$UD/alive"
  if [ -n "${HB:-}" ]; then
    pkill -P "$HB" 2>/dev/null || true
    kill "$HB" 2>/dev/null || true
  fi
  ua_wt_remove
  rm -f "$UD/state"
}

# Worktree прогона: каталог build со ссылкой build/router на основную копию, node_modules.
# Код 1 — worktree не создан.
ua_prepare() {
  WT="$WT_BASE/wt-$U_SHORT"
  if [ -e "$WT" ]; then
    echo "остался worktree прошлого прогона — снимается: $WT"
    ua_wt_remove
  fi
  [ ! -e "$WT" ] || return 1
  mkdir -p "$WT_BASE" "$ROOT"/build/router
  git worktree add --detach --lock --reason router-test-unattended "$WT" "$U_SHA" || return 1
  mkdir -p "$WT/build"
  ln -sfn "$ROOT"/build/router "$WT/build"/router
  if [ -d "$ROOT/frontend/node_modules" ] && cmp -s "$ROOT/frontend/package-lock.json" "$WT/frontend/package-lock.json"; then
    ln -s "$ROOT/frontend/node_modules" "$WT/frontend/node_modules"
  else
    echo "package-lock.json отличается от основной копии: npm ci"
    npm --prefix "$WT/frontend" ci || echo "npm ci не удался"
  fi
}

# Новые падения отчёта: строки «проверка (ядро)». Класс new из <цель>/classes.tsv, иначе FAIL и
# XPASS из <цель>/results.tsv.
ua_new_list() {
  for _nd in "$1"/*/; do
    [ -f "$_nd/results.tsv" ] || continue
    if [ -f "$_nd/classes.tsv" ]; then
      awk -F'\t' '$1 == "new" { print $3 " (" $2 ")" }' "$_nd/classes.tsv"
    else
      awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" { print $3 " (" $2 ")" }' "$_nd/results.tsv"
    fi
  done | rt_redact
}

# Непройденное: этапы с кодом 3 или 130, SKIP «нет связи», <цель>/notrun.txt, строки notrun
# <цель>/version-stop.tsv (не выполнено после остановки на смене версии стенда).
ua_notrun() {
  [ ! -f "$1/timings.tsv" ] ||
    awk -F'\t' '$4 == 3 || $4 == 130 { print "этап " $2 " (" $1 ", код " $4 ")" }' "$1/timings.tsv"
  for _nd in "$1"/*/; do
    [ -d "$_nd" ] || continue
    _nt=$(basename "$_nd")
    [ ! -f "$_nd/results.tsv" ] ||
      awk -F'\t' -v t="$_nt" '$1 == "SKIP" && $4 ~ /нет связи/ { print t ": " $3 " (" $2 ") — нет связи" }' "$_nd/results.tsv"
    [ ! -s "$_nd/notrun.txt" ] || sed "s/^/$_nt: /" "$_nd/notrun.txt"
    [ ! -f "$_nd/version-stop.tsv" ] ||
      awk -F'\t' -v t="$_nt" '$1 == "notrun" && $3 != "" { print t ": " $3 " (" $2 ") — смена версии стенда" }' "$_nd/version-stop.tsv"
  done | rt_redact
}

# Остановка на смене версии стенда: первая строка stop из <отчёт>/<цель>/version-stop.tsv —
# «<цель>\t<ядро>\t<граница>\t<ожидалась>\t<на стенде>»; остановки нет — пусто.
ua_vstop() {
  for _vd in "$1"/*/; do
    [ -f "$_vd/version-stop.tsv" ] || continue
    _vl=$(awk -F'\t' -v t="$(basename "$_vd")" '$1 == "stop" { print t "\t" $2 "\t" $3 "\t" $4 "\t" $5; exit }' "$_vd/version-stop.tsv")
    if [ -n "$_vl" ]; then
      printf '%s\n' "$_vl"
      return 0
    fi
  done
}

# Строка итога отчёта: «- Итог:» summary.md, иначе счётчики целей.
ua_itog() {
  _is="$1/summary.md"
  if [ ! -f "$_is" ]; then
    echo "summary.md нет"
    return 0
  fi
  _il=$(grep -m 1 '^- Итог:' "$_is" || true)
  if [ -n "$_il" ]; then
    printf '%s\n' "${_il#- Итог: }"
    return 0
  fi
  grep -E 'passed [0-9]+, failed [0-9]+' "$_is" | sed -E 's/^([a-z0-9]+): xcp [^,]*, /\1: /' | paste -sd ';' - | sed 's/;/; /g'
}

# Строки итога latest.md, зависящие от отчёта: «- Итог:», «- Причина остановки:» (смена версии
# стенда), «- Код run.sh:» (только если передан второй аргумент), «- Новые падения:» и список,
# «- Итог bisect», «- Не выполнено:» и список. ua_report_lines <отчёт> [<код run.sh>]
ua_report_lines() {
  _rl=$1
  echo "- Итог: $(ua_itog "$_rl")"
  _rv=$(ua_vstop "$_rl")
  if [ -n "$_rv" ]; then
    printf '%s\n' "$_rv" |
      awk -F'\t' '{ print "- Причина остановки: смена версии стенда — ожидалась " $4 ", на стенде " $5 ", граница " $3 " (" $1 ", " $2 ")" }' |
      rt_redact
  fi
  [ -z "${2:-}" ] || echo "- Код run.sh: $2"
  _rn=$(ua_new_list "$_rl")
  echo "- Новые падения: $(printf '%s' "$_rn" | grep -c . || true)"
  [ -z "$_rn" ] || printf '%s\n' "$_rn" | head -n 30 | sed 's/^/  - /'
  [ ! -f "$_rl/summary.md" ] || grep '^- Итог bisect' "$_rl/summary.md" || true
  _rnr=$(ua_notrun "$_rl")
  if [ -n "$_rnr" ]; then
    echo "- Не выполнено:"
    printf '%s\n' "$_rnr" | head -n 30 | sed 's/^/  - /'
  fi
}

# Подкоманда report: строки итога latest.md по отчёту в stdout, без записи файлов. Отчёт — каталог
# под build/router основной копии.
ua_report() {
  [ $# -eq 1 ] && [ -n "$1" ] || ua_die "report: ожидается один путь отчёта (report <отчёт>)"
  [ -d "$1" ] || ua_die "report: нет каталога отчёта $1"
  _rp=$(cd "$1" && pwd -P) || ua_die "report: каталог отчёта не читается"
  _rb=$(cd "$ROOT/build/router" 2>/dev/null && pwd -P) || ua_die "report: нет build/router основной копии"
  case "$_rp" in
    "$_rb"/?*) ;;
    *) ua_die "report: отчёт вне build/router основной копии" ;;
  esac
  ua_report_lines "$_rp"
}

ua_run() {
  cd "$ROOT"
  RUN=${1:-}
  case "$RUN" in
    "$UD"/runs/?*) ;;
    *) ua_die "run: каталог вне каталога прогонов" ;;
  esac
  case "/${RUN#"$UD"/runs/}/" in
    */../* | */./* | *//*) ua_die "run: недопустимый каталог" ;;
  esac
  [ -f "$RUN/meta" ] || ua_die "run: нет meta"
  U_REF=$(rt_kv ref <"$RUN/meta")
  U_SHA=$(rt_kv sha <"$RUN/meta")
  U_REL=$(rt_kv release <"$RUN/meta")
  U_AT=$(rt_kv at <"$RUN/meta")
  ua_match "$REF_RE" "$U_REF" || ua_die "run: недопустимый ref"
  ua_match '^[0-9a-f]{40}$' "$U_SHA" || ua_die "run: недопустимый sha"
  [ "$U_REL" = - ] || ua_match "$TAG_RE" "$U_REL" || ua_die "run: недопустимый release"
  [ "$U_AT" = - ] || ua_match "$AT_RE" "$U_AT" || ua_die "run: недопустимое at"
  U_SHORT=$(git rev-parse --short "$U_SHA")
  U_STARTED=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  _relp=""
  [ "$U_REL" = - ] || _relp=$U_REL

  # Отладочные переменные вызывающего (все RT_*, выбор целей, спека) на ночной прогон не влияют:
  # он идёт полной матрицей на целях из локального конфига.
  for _v in $(env | sed -n 's/^\(RT_[A-Za-z0-9_]*\)=.*/\1/p'); do
    unset "$_v" 2>/dev/null || true
  done
  unset ROUTERS SPEC SUITE REF RELEASE AT 2>/dev/null || true

  HB=""
  WT=""
  trap 'ua_cleanup' EXIT
  trap 'exit 130' INT TERM HUP
  ua_state ожидание
  : >"$UD/alive"
  (
    while [ -e "$UD/alive" ]; do
      printf '%s\t%s\n' "$(date +%s)" "$(date '+%Y-%m-%d %H:%M:%S')" >>"$RUN/heartbeat.tsv"
      sleep 60
    done
  ) &
  HB=$!
  ua_hold
  ua_wait_at

  ua_state подготовка
  _prep=""
  ua_prepare || _prep="worktree не создан (подробности — в log прогона)"

  _rc=0
  _t0=$(date +%s)
  if [ -z "$_prep" ]; then
    ua_state прогон
    (cd "$WT" && RT_LOCK_WAIT=7200 RELEASE="$_relp" sh scripts/router/run.sh full) >"$RUN/run.log" 2>&1 || _rc=$?
  else
    : >"$RUN/run.log"
  fi
  _t1=$(date +%s)
  _date=$(date +%Y-%m-%d)

  # Отчёт — из строки «router-test: отчёт <путь>» вывода run.sh, только каталог отчёта основной копии
  _report=""
  _rn=$(sed -n 's/^router-test: отчёт //p' "$RUN/run.log" | head -n 1)
  if [ -n "$_rn" ]; then
    _rb=$(basename "$_rn")
    if ua_match '^[0-9]{8}-[0-9]{6}-[0-9a-f]+(-dirty)?$' "$_rb" && [ -d "$ROOT"/build/router/"$_rb" ]; then
      _report="$ROOT"/build/router/"$_rb"
    fi
  fi

  _new=0
  _newlist=""
  _hist=""
  if [ -n "$_prep" ]; then
    _verdict="не начат"
    _itog="не начат: $_prep"
    _unread="полный прогон $_date: красный, новых падений 0"
  elif [ -z "$_report" ] && grep -q 'очередь на роутеры не освободилась' "$RUN/run.log"; then
    _verdict=пропущен
    _itog="пропущен: стенд занят (очередь на роутеры не освободилась за 7200 с)"
    _unread="полный прогон $_date: пропущен"
  elif [ -z "$_report" ]; then
    _verdict="не начат"
    _itog="не начат: $(tail -n 1 "$RUN/run.log" | cut -c1-200 | rt_redact)"
    _unread="полный прогон $_date: красный, новых падений 0"
  else
    # Остановка на смене версии стенда (D-13): строка stop в <цель>/version-stop.tsv отчёта. Вердикт
    # «остановлен» не входит в правило пропуска (ua_last_done), поэтому следующий запуск с тем же SHA идёт
    _vstop=$(ua_vstop "$_report")
    if [ -n "$_vstop" ]; then
      _verdict=остановлен
    else
      case "$_rc" in
        0) _verdict=зелёный ;;
        130) _verdict=прерван ;;
        *) _verdict=красный ;;
      esac
    fi
    _itog=$(ua_itog "$_report")
    _newlist=$(ua_new_list "$_report")
    _new=$(printf '%s' "$_newlist" | grep -c . || true)
    if [ -n "$_vstop" ]; then
      _unread="полный прогон $_date: остановлен — смена версии стенда"
    elif [ "$_rc" = 0 ]; then
      _unread="полный прогон $_date: зелёный, новых падений $_new"
    else
      _unread="полный прогон $_date: красный, новых падений $_new"
    fi
    # История (D-20): база добора и bisect ищет прогон «до» в ней. Прерванный и остановленный сменой
    # версии прогоны не пишутся (как и в run.sh, где выход 130 раньше записи): частичный прогон без
    # падений стал бы «зелёным полным»
    if [ -n "$_vstop" ]; then
      _hist="история: не записано (смена версии стенда — прогон неполный)"
    elif [ "$_rc" = 130 ]; then
      _hist="история: не записано (прогон прерван)"
    elif [ -f "$ROOT/scripts/router/history.mjs" ]; then
      if node "$ROOT/scripts/router/history.mjs" append "$_report"; then
        _hist="история: записано"
      else
        _hist="история: ошибка записи (подробности — в log прогона)"
      fi
    else
      _hist="история: не записано — выполнить node scripts/router/history.mjs append $_report"
    fi
  fi

  _gap=0
  [ ! -f "$RUN/heartbeat.tsv" ] ||
    _gap=$(awk -F'\t' 'NR > 1 && $1 - p > m { m = $1 - p } { p = $1 } END { print m + 0 }' "$RUN/heartbeat.tsv")

  {
    echo "# Полный прогон без присмотра"
    echo
    echo "- Дата: $_date, начало $(date -d "@$_t0" '+%H:%M'), длительность $(((_t1 - _t0) / 60)) мин"
    echo "- ref: $U_REF, sha: $U_SHA"
    echo "- Релиз: $U_REL"
    echo "- Отчёт: ${_report:--}"
    if [ -n "$_report" ]; then
      ua_report_lines "$_report" "$_rc"
      echo "- $_hist"
    else
      echo "- Итог: $_itog"
      echo "- Код run.sh: $_rc"
    fi
    echo "- Наибольший разрыв пульса: $_gap с"
    echo "- Лог: $RUN/log"
  } >"$UD/latest.md.tmp"
  mv -f "$UD/latest.md.tmp" "$UD/latest.md"
  printf '%s\n' "$_unread" >"$UD/unread"
  {
    echo "verdict=$_verdict"
    echo "rc=$_rc"
    echo "report=${_report:--}"
    echo "finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >>"$RUN/meta"
  echo "итог: $_itog"
}

# --- status -------------------------------------------------------------------------------

ua_status() {
  if _sp=$(ua_alive_pid); then
    _sph=$(rt_kv phase <"$UD/state")
    _srun=$(rt_kv run <"$UD/state")
    case "$_sph" in
      ожидание) _sst=ожидание ;;
      прогон)
        if [ -f "$_srun/run.log" ] && grep -q 'очередь на роутеры — занято' "$_srun/run.log" &&
          ! grep -q '^router-test: отчёт ' "$_srun/run.log"; then
          _sst=очередь
        else
          _sst=идёт
        fi
        ;;
      *) _sst=идёт ;;
    esac
    echo "состояние: $_sst"
    for _sk in ref sha release at; do
      echo "$_sk=$(rt_kv "$_sk" <"$UD/state")"
    done
    echo "лог=$_srun/log"
  else
    echo "состояние: нет"
    for _sk in ref sha release at; do
      echo "$_sk=-"
    done
    echo "лог=-"
    [ ! -f "$UD/latest.md" ] || echo "итог=$UD/latest.md"
  fi
}

_cmd=${1:-start}
[ $# -eq 0 ] || shift
case "$_cmd" in
  start) ua_start "$@" ;;
  status) ua_status ;;
  run) ua_run "$@" ;;
  report) ua_report "$@" ;;
  *)
    echo "router-test-unattended: неизвестная подкоманда '$_cmd' (start [--dry-run] | status | report <отчёт>)" >&2
    exit 2
    ;;
esac
