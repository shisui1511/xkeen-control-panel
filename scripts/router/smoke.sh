#!/bin/sh
# scripts/router/smoke.sh <id цели> <ожидаемая версия xcp> [--from-line <n>] — смоук цели после деплоя.
#
# Проверки (каждая пишет строку в <отчёт>/<id>/results.tsv; D-34):
#   kernel-running       xkeen -status сообщает «запущен в режиме …», процесс ядра жив;
#                        ядро записывается в <отчёт>/<id>/core
#   iptables-rules       при запущенном XKeen есть его правила TPROXY (mangle) и/или
#                        REDIRECT (nat) — по режиму XKeen; XKeen ставит правила в
#                        фоне уже после запуска, поэтому проверка ждёт их до
#                        RT_SMOKE_WINDOW с и только потом краснеет
#   version              /opt/sbin/xcp -v равна ожидаемой версии
#   xcp-pid              у pidof xcp ровно один процесс
#   api-version          GET <панель>/api/version с ПК отвечает 200
#   api-me               вход с ПК и GET /api/auth/me = 200, затем выход
#   api-service-status   GET /api/service/status = 200
#   kernel-restart       PID ядра в начале и в конце окна RT_SMOKE_WINDOW (по умолчанию
#                        120 с) один и тот же: смена PID без команды — падение
#   log-panic            в xcp.log после последнего баннера запуска нет panic
#   log-error            в xcp.log после баннера нет слов ERROR и fatal
#   log-repeat:<префикс> сообщение, повторившееся не менее 3 раз и чаще раза в минуту
#                        (строки «[dedup] previous message repeated N time(s)» учтены;
#                        вход и выход самого смоука в счёт не идут)
#
# --from-line <n>: правила логов смотрят строки xcp.log после строки n, а не после
# последнего баннера запуска (для следующих ветвей матрицы).
#
# Известные падения (D-12): FAIL, совпавший с записью scripts/router/known-failures
# (smoke:<проверка или префикс>, селектор цели, slug todo), записывается как KNOWN;
# применимая запись без совпавшего FAIL — XPASS (метка больше не нужна), прогон красный.
#
# Хвосты логов в отчёте проходят rt_redact (адреса и токены скрыты).
#
# Код выхода: 0 — нет FAIL и XPASS, 3 — цель недоступна (reachable), иначе 1.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"

usage() {
  echo "usage: smoke.sh <id цели> <ожидаемая версия> [--from-line <n>]" >&2
  exit 2
}

[ $# -ge 2 ] || usage
ID=$1
EXPECT=$2
shift 2
FROM_LINE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --from-line)
      [ $# -ge 2 ] || usage
      FROM_LINE=$2
      case "$FROM_LINE" in
        '' | *[!0-9]*) usage ;;
      esac
      shift 2
      ;;
    *) usage ;;
  esac
done

cd "$(rt_repo_root)"
ROUTERS=$ID
export ROUTERS
rt_load_targets
[ -n "${RT_REPORT:-}" ] || rt_report_init
mkdir -p "$RT_REPORT/$ID"

ARCH=$(rt_get "$ID" ARCH)
TAB=$(printf '\t')
ESC=$(printf '\033')
WINDOW=${RT_SMOKE_WINDOW:-120}
LOG_PATH=/opt/var/log/xcp.log
KNOWN_FILE="$SCRIPT_DIR/known-failures"
BUF="$RT_REPORT/$ID/smoke.buf"
: >"$BUF"
rm -f "$RT_REPORT/$ID/core"
UNREACH=0

# Строка результата в буфер; в results.tsv она попадает через finalize (метки известных падений).
chk() {
  printf '%s\t%s\t%s\n' "$1" "$2" "$(printf '%s' "${3:-}" | tr '\t\n' '  ')" >>"$BUF"
}

# ssh с учётом потери связи: rc 3 -> FAIL reachable, остальное пропущено.
lost() {
  UNREACH=1
  chk FAIL reachable "нет связи по ssh"
  exit 3
}

# Итог: метки известных падений -> results.tsv, код выхода.
finalize() {
  _rc=$?
  trap - EXIT
  if [ "$UNREACH" = 1 ]; then
    for _c in kernel-running iptables-rules version xcp-pid api-version api-me api-service-status kernel-restart log-panic log-error; do
      grep -q "^[A-Z]*${TAB}${_c}${TAB}" "$BUF" || chk SKIP "$_c" "нет связи"
    done
  fi
  _core="-"
  [ -f "$RT_REPORT/$ID/core" ] && _core=$(cat "$RT_REPORT/$ID/core")
  _lab="$RT_REPORT/$ID/smoke.labels"
  : >"$_lab"
  if [ -f "$KNOWN_FILE" ]; then
    while IFS="$TAB" read -r _lchk _lsel _lslug || [ -n "$_lchk" ]; do
      case "$_lchk" in smoke:?*) ;; *) continue ;; esac
      [ -n "$_lsel" ] && [ -n "$_lslug" ] || continue
      rt_selector_match "$_lsel" "$ARCH" "$_core" || continue
      printf '%s\t%s\n' "${_lchk#smoke:}" "$_lslug" >>"$_lab"
    done <"$KNOWN_FILE"
  fi
  # FAIL, совпавший с применимой меткой (точно или по префиксу), -> KNOWN; метка без
  # совпавшего FAIL -> XPASS.
  _fin="$RT_REPORT/$ID/smoke.final"
  awk -F'\t' -v OFS='\t' -v lf="$_lab" '
    BEGIN { n = 0 }
    FILENAME == lf { lp[n] = $1; ls[n] = $2; n++; next }
    {
      st = $1; det = $3
      if (st == "FAIL") {
        for (i = 0; i < n; i++) {
          if (index($2, lp[i]) == 1) {
            st = "KNOWN"; det = det " [известное падение: " ls[i] "]"; m[i] = 1; break
          }
        }
      }
      print st, $2, det
    }
    END {
      for (i = 0; i < n; i++)
        if (!m[i]) print "XPASS", "known:" lp[i], "метка больше не нужна: снять и закрыть todo " ls[i]
    }
  ' "$_lab" "$BUF" >"$_fin"
  while IFS="$TAB" read -r _st _chk _det || [ -n "$_st" ]; do
    rt_result "$ID" "$_st" "$_chk" "$_det"
  done <"$_fin"
  _bad=$(awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" {n++} END {print n + 0}' "$_fin")
  if [ "$_rc" = 3 ]; then
    exit 3
  elif [ "$_rc" != 0 ] || [ "$_bad" != 0 ]; then
    exit 1
  fi
  exit 0
}
trap finalize EXIT

# --- kernel-running ----------------------------------------------------------
rc=0
out=$(rt_stage "$ID" smoke-kernel rt_ssh "$ID" '/opt/sbin/xkeen -status 2>&1; echo "xray_pid=$(pidof xray | cut -d" " -f1)"; echo "mihomo_pid=$(pidof mihomo | cut -d" " -f1)"') || rc=$?
[ "$rc" != 3 ] || lost
clean=$(printf '%s\n' "$out" | sed "s/${ESC}\[[0-9;]*[A-Za-z]//g")
xpid=$(printf '%s\n' "$clean" | sed -n 's/^xray_pid=//p')
mpid=$(printf '%s\n' "$clean" | sed -n 's/^mihomo_pid=//p')
run_line=$(printf '%s\n' "$clean" | grep 'запущен в режиме' | head -n 1 || true)
mode=$(printf '%s' "$run_line" | sed -n 's/.*запущен в режиме *//p' | tr -d ' ')
core=""
case "$run_line" in
  *xray*) core=xray ;;
  *mihomo*) core=mihomo ;;
esac
if [ -z "$core" ]; then
  if [ -n "$xpid" ] && [ -z "$mpid" ]; then
    core=xray
  elif [ -n "$mpid" ] && [ -z "$xpid" ]; then
    core=mihomo
  fi
fi
[ -z "$core" ] || printf '%s\n' "$core" >"$RT_REPORT/$ID/core"
core_pid=""
case "$core" in
  xray) core_pid=$xpid ;;
  mihomo) core_pid=$mpid ;;
esac
if [ -n "$run_line" ] && [ -n "$core_pid" ]; then
  chk PASS kernel-running "$core запущен, режим $mode"
else
  chk FAIL kernel-running "XKeen не сообщает о запущенном ядре или процесс ядра не найден (ядро ${core:--}, режим ${mode:--})"
fi

# --- iptables-rules ----------------------------------------------------------
if [ -n "$run_line" ]; then
  # XKeen возвращает управление раньше, чем фоновый запуск поставил правила перехвата
  # (сразу после смены ядра правил ещё нет): проверка опрашивает роутер раз в
  # IPT_POLL с, но не дольше окна RT_SMOKE_WINDOW. Правил нет и по истечении окна — FAIL.
  IPT_POLL=3
  ipt_wait=$WINDOW
  [ "$ipt_wait" -gt 0 ] 2>/dev/null || ipt_wait=0
  ipt_t0=$(date +%s)
  while :; do
    rc=0
    # только -w без числа: iptables 1.4.21 не принимает -w 5
    out=$(rt_stage "$ID" smoke-iptables rt_ssh "$ID" 'echo "tproxy=$(iptables -w -t mangle -S 2>/dev/null | grep -c "xkeen.*-j TPROXY")"; echo "redirect=$(iptables -w -t nat -S 2>/dev/null | grep -c "xkeen.*-j REDIRECT")"') || rc=$?
    [ "$rc" != 3 ] || lost
    nt=$(printf '%s\n' "$out" | sed -n 's/^tproxy=//p')
    nr=$(printf '%s\n' "$out" | sed -n 's/^redirect=//p')
    nt=${nt:-0}
    nr=${nr:-0}
    ok=0
    case "$mode" in
      TProxy | tproxy) [ "$nt" -gt 0 ] && ok=1 ;;
      Redirect | redirect) [ "$nr" -gt 0 ] && ok=1 ;;
      Hybrid | hybrid) [ "$nt" -gt 0 ] && [ "$nr" -gt 0 ] && ok=1 ;;
      *) { [ "$nt" -gt 0 ] || [ "$nr" -gt 0 ]; } && ok=1 ;;
    esac
    [ "$ok" = 1 ] && break
    ipt_waited=$(($(date +%s) - ipt_t0))
    [ "$ipt_waited" -lt "$ipt_wait" ] || break
    sleep "$IPT_POLL"
  done
  ipt_waited=$(($(date +%s) - ipt_t0))
  ipt_note=""
  [ "$ipt_waited" -lt 2 ] || ipt_note=", ожидание ${ipt_waited} с"
  if [ "$ok" = 1 ]; then
    chk PASS iptables-rules "режим ${mode:--}: TPROXY $nt, REDIRECT $nr$ipt_note"
  else
    chk FAIL iptables-rules "режим ${mode:--}: правил XKeen TPROXY $nt, REDIRECT $nr — не хватает$ipt_note"
  fi
else
  chk SKIP iptables-rules "XKeen не запущен"
fi

# --- version -----------------------------------------------------------------
rc=0
got=$(rt_stage "$ID" smoke-version rt_ssh "$ID" "/opt/sbin/xcp -v") || rc=$?
[ "$rc" != 3 ] || lost
if [ "$rc" != 0 ]; then
  chk FAIL version "xcp -v завершился с кодом $rc"
elif [ "$got" = "$EXPECT" ]; then
  chk PASS version "$got"
  printf '%s\n' "$got" >"$RT_REPORT/$ID/xcp_version"
else
  chk FAIL version "на цели $got, ожидалась $EXPECT"
  printf '%s\n' "$got" >"$RT_REPORT/$ID/xcp_version"
fi

# --- xcp-pid -----------------------------------------------------------------
rc=0
pids=$(rt_stage "$ID" smoke-pid rt_ssh "$ID" "pidof xcp") || rc=$?
[ "$rc" != 3 ] || lost
count=$(printf '%s' "$pids" | wc -w | tr -d ' ')
if [ "$rc" = 0 ] && [ "$count" = 1 ]; then
  chk PASS xcp-pid "1 процесс"
else
  chk FAIL xcp-pid "процессов xcp: $count"
fi

# --- api-version -------------------------------------------------------------
url=$(rt_get "$ID" URL)
t0=$(date +%s)
# Панель после перезапуска xcp может ещё не слушать порт: до 30 с ждём любой ответ.
code=000
tries=0
while [ "$tries" -lt 30 ]; do
  code=$(curl -sk --connect-timeout 5 --max-time 20 -o /dev/null -w '%{http_code}' "$url/api/version" || true)
  [ "$code" != 000 ] && [ -n "$code" ] && break
  tries=$((tries + 1))
  sleep 1
done
t1=$(date +%s)
printf '%s\t%s\t%s\t%s\n' "$ID" smoke-api "$((t1 - t0))" 0 >>"$RT_REPORT/timings.tsv"
if [ "$code" = 200 ]; then
  chk PASS api-version "http 200"
elif [ "$code" = 000 ] || [ -z "$code" ]; then
  chk FAIL api-version "нет связи с панелью"
else
  chk FAIL api-version "http $code"
fi

# --- api-me, api-service-status ----------------------------------------------
# Вход с ПК: тело строится через JSON.stringify (пароль идёт через окружение и stdin,
# в аргументы команд не попадает). Сессия закрывается выходом: лимит 20 сессий.
jar="$RT_REPORT/$ID/.cookies"
rm -f "$jar"
t0=$(date +%s)
body=$(RT_PW="$(rt_get "$ID" PASSWORD)" node -e 'process.stdout.write(JSON.stringify({ password: process.env.RT_PW }))' || true)
lcode=$(printf '%s' "$body" | curl -sk --connect-timeout 10 --max-time 20 -c "$jar" -H 'Content-Type: application/json' --data-binary @- -o /dev/null -w '%{http_code}' "$url/api/auth/login" || true)
body=""
if [ "$lcode" = 200 ]; then
  me=$(curl -sk --connect-timeout 10 --max-time 20 -b "$jar" -w '\n%{http_code}' "$url/api/auth/me" || true)
  mecode=$(printf '%s\n' "$me" | tail -n 1)
  csrf=$(printf '%s\n' "$me" | sed -n 's/.*"csrf_token" *: *"\([^"]*\)".*/\1/p' | head -n 1)
  if [ "$mecode" = 200 ]; then
    chk PASS api-me "вход и GET /api/auth/me: http 200"
  else
    chk FAIL api-me "GET /api/auth/me: http ${mecode:--}"
  fi
  sscode=$(curl -sk --connect-timeout 10 --max-time 30 -b "$jar" -o /dev/null -w '%{http_code}' "$url/api/service/status" || true)
  if [ "$sscode" = 200 ]; then
    chk PASS api-service-status "http 200"
  else
    chk FAIL api-service-status "GET /api/service/status: http ${sscode:--}"
  fi
  curl -sk --connect-timeout 10 --max-time 20 -b "$jar" -X POST -H "X-CSRF-Token: $csrf" -o /dev/null "$url/api/auth/logout" || true
else
  chk FAIL api-me "вход не удался: http ${lcode:--}"
  chk SKIP api-service-status "нет сессии"
fi
rm -f "$jar"
t1=$(date +%s)
printf '%s\t%s\t%s\t%s\n' "$ID" smoke-api-session "$((t1 - t0))" 0 >>"$RT_REPORT/timings.tsv"

# --- kernel-restart: PID ядра в начале и конце окна ----------------------------
if [ -z "$core" ]; then
  chk SKIP kernel-restart "активное ядро не определено"
elif [ "$WINDOW" -le 0 ] 2>/dev/null; then
  chk SKIP kernel-restart "окно наблюдения отключено (RT_SMOKE_WINDOW=0)"
else
  # XKeen после запуска или переключения ядра на медленном устройстве заменяет процесс
  # и выходит на рабочее состояние не сразу: окно начинается, когда у ядра один и тот же
  # единственный PID продержался 30 с (не дольше 120 с; не дождались — окно покажет).
  rc=0
  rt_stage "$ID" smoke-settle rt_ssh "$ID" "i=0; last=; same=0; while [ \$i -lt 120 ]; do p=\$(pidof $core); case \"\$p\" in ''|*' '*) last=; same=0 ;; *) if [ \"\$p\" = \"\$last\" ]; then same=\$((same + 5)); else last=\$p; same=0; fi ;; esac; [ \$same -ge 30 ] && break; sleep 5; i=\$((i + 5)); done" || rc=$?
  [ "$rc" != 3 ] || lost
  rc=0
  p0=$(rt_ssh "$ID" "pidof $core") || rc=$?
  [ "$rc" != 3 ] || lost
  rt_stage "$ID" smoke-window sleep "$WINDOW"
  rc=0
  p1=$(rt_ssh "$ID" "pidof $core") || rc=$?
  [ "$rc" != 3 ] || lost
  if [ -n "$p0" ] && [ "$p0" = "$p1" ]; then
    chk PASS kernel-restart "PID $core не менялся за $WINDOW с"
  else
    chk FAIL kernel-restart "PID $core за $WINDOW с: было '${p0:--}', стало '${p1:--}' — перезапуск без команды"
  fi
fi

# --- правила логов xcp.log -----------------------------------------------------
# Хвост после последнего баннера (или после строки --from-line) забирается одним
# вызовом и сразу проходит rt_redact; анализ идёт по обезличенному файлу.
tail_file="$RT_REPORT/$ID/xcp-log-tail.txt"
REMOTE_LOG='
L=/opt/var/log/xcp.log
[ -f "$L" ] || exit 9
from=$1
if [ -z "$from" ]; then
  from=$(grep -n "XKeen Control Panel v.* starting\.\.\." "$L" | tail -n 1 | cut -d: -f1)
  from=${from:-0}
fi
sed -n "$((from + 1)),\$p" "$L"
'
rc=0
printf '%s\n' "$REMOTE_LOG" | RT_SSH_STDIN=1 rt_ssh "$ID" "sh -s -- $FROM_LINE" 2>/dev/null | rt_redact >"$tail_file" || rc=$?
# rc здесь — код rt_redact; кода ssh в POSIX sh не получить, поэтому отсутствие
# лога определяется по пустому файлу и повторной проверкой наличия файла.
if [ ! -s "$tail_file" ]; then
  rc=0
  rt_ssh "$ID" "[ -f $LOG_PATH ]" || rc=$?
  [ "$rc" != 3 ] || lost
  if [ "$rc" != 0 ]; then
    chk FAIL log-panic "нет файла $LOG_PATH"
    chk FAIL log-error "нет файла $LOG_PATH"
  else
    chk PASS log-panic "после баннера запуска строк нет"
    chk PASS log-error "после баннера запуска строк нет"
    chk PASS log-repeat "после баннера запуска строк нет"
  fi
else
  lines=$(wc -l <"$tail_file" | tr -d ' ')
  pan=$(grep -i 'panic' "$tail_file" | head -n 3 | cut -c1-200 | tr '\n' '|' || true)
  if [ -z "$pan" ]; then
    chk PASS log-panic "строк проверено: $lines, panic нет"
  else
    chk FAIL log-panic "panic в xcp.log: $pan"
  fi
  err=$(grep -E '(^|[^A-Za-z])ERROR([^A-Za-z]|$)|(^|[^A-Za-z])[Ff][Aa][Tt][Aa][Ll]([^A-Za-z]|$)' "$tail_file" | head -n 3 | cut -c1-200 | tr '\n' '|' || true)
  if [ -z "$err" ]; then
    chk PASS log-error "строк проверено: $lines, ERROR и fatal нет"
  else
    chk FAIL log-error "ERROR или fatal в xcp.log: $err"
  fi
  # Повторы: нормализация (метка времени убрана, числа -> 0, адреса/hex/uuid -> маски),
  # строка «[dedup] ... repeated N time(s)» засчитывается предыдущему сообщению.
  reps=$(awk '
    function days(y, m, d) {
      if (m <= 2) { y--; m += 12 }
      return 365 * y + int(y / 4) - int(y / 100) + int(y / 400) + int((153 * (m - 3) + 2) / 5) + d
    }
    {
      if (match($0, /^[0-9][0-9][0-9][0-9]\/[0-9][0-9]\/[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9] /)) {
        ts = days(substr($0, 1, 4) + 0, substr($0, 6, 2) + 0, substr($0, 9, 2) + 0) * 86400 + substr($0, 12, 2) * 3600 + substr($0, 15, 2) * 60 + substr($0, 18, 2)
        if (first == "") first = ts
        last = ts
        msg = substr($0, 21)
      } else {
        msg = $0
      }
      if (msg ~ /^\[dedup\] previous message repeated [0-9]+ time\(s\)/) {
        s = msg
        sub(/^\[dedup\] previous message repeated /, "", s)
        sub(/ .*/, "", s)
        if (prev != "") cnt[prev] += s + 0
        next
      }
      # собственный вход и выход смоука (и проверочных прогонов) — не повтор сообщения продукта
      if (msg ~ /^\[auth\] (login ok|logout) / || msg ~ /^(POST|GET) \/api\/auth\/(login|logout|me) /) {
        prev = ""
        next
      }
      gsub(/\t/, " ", msg)
      gsub(/[0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+/, "<uuid>", msg)
      gsub(/[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+(:[0-9]+)?/, "<ip>", msg)
      gsub(/[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]+/, "<hex>", msg)
      gsub(/[0-9]+/, "0", msg)
      cnt[msg]++
      prev = msg
    }
    END {
      span = last - first
      if (span < 60) span = 60
      for (k in cnt)
        if (cnt[k] >= 3 && cnt[k] * 60 > span)
          printf "%d\t%d\t%s\n", cnt[k], span, k
    }
  ' "$tail_file" | sort -rn | head -n 20)
  if [ -z "$reps" ]; then
    chk PASS log-repeat "сообщений, повторяющихся чаще раза в минуту, нет"
  else
    printf '%s\n' "$reps" | while IFS="$TAB" read -r rcnt rspan rmsg; do
      chk FAIL "log-repeat:$(printf '%s' "$rmsg" | cut -c1-40)" "повторов $rcnt за $rspan с: $(printf '%s' "$rmsg" | cut -c1-120)"
    done
  fi
fi
exit 0
