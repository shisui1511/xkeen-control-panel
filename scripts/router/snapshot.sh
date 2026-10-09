#!/bin/sh
# scripts/router/snapshot.sh — снимок, восстановление и сверка состояния роутера.
#
# Выполняется НА УСТРОЙСТВЕ (POSIX sh, busybox ash/Entware). lib.sh копирует файл
# в /opt/tmp/xcp-rt-snap/ и вызывает подкоманды по ssh. Содержимое конфигов в
# вывод не печатается — только пути, md5 и счётчики.
#
# Подкоманды:
#   preflight   GNU tar на месте (выход 5), нет окна cron xkeen -ug (выход 6),
#               нет снимка незавершённого прогона (выход 7)
#   snapshot    остановить xcp, снять архив и md5 (до трёх попыток), записать
#               версии и активное ядро, запустить xcp
#   restore     вернуть файлы и ядро к снимку со строгой сверкой (выход 4 — не удалось)
#   verify      сверка с снимком: 0 — равно, 1 — расхождение
#   cleanup     убрать снимок (после успешной сверки)
#   status      версии, активное ядро, процессы, есть ли снимок
#
# Восстановление удаляет файлы только внутри /opt и только из фиксированного списка
# SNAP_PATHS; всё вне /opt не затрагивается.

PATH=/opt/sbin:/opt/bin:/opt/usr/sbin:/opt/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

D=/opt/tmp/xcp-rt-snap
BASE=$D/baseline
WORK=$D/work
XKEEN=/opt/sbin/xkeen
INITD_XCP=/opt/etc/init.d/S99xcp
TAR=/opt/bin/tar
FIND=/opt/bin/find
TAB=$(printf '\t')
ESC=$(printf '\033')

SNAP_PATHS="/opt/etc/xray /opt/etc/mihomo /opt/etc/xkeen /opt/etc/xcp /opt/etc/init.d /opt/etc/ndm /opt/sbin/xray /opt/sbin/mihomo /opt/sbin/xkeen /opt/sbin/.xkeen /opt/sbin/xcp /opt/var/spool/cron/crontabs"

# Бинарники из архива исключены: в снимке на них жёсткие ссылки ($BASE/bin, тот же раздел),
# архив остаётся маленьким, а запись на флешку — короткой. Замена файла переименованием
# оставляет ссылке прежнее содержимое; запись «на месте» ловится сверкой md5 при восстановлении.
BIN_PATHS="/opt/sbin/xray /opt/sbin/mihomo /opt/sbin/xcp"

mkdir -p "$WORK" 2>/dev/null
chmod 700 "$D" 2>/dev/null

say() { printf '%s\n' "$*"; }
strip_ansi() { sed "s/${ESC}\[[0-9;]*[A-Za-z]//g"; }

# ---------- перечисление и md5 ----------

existing_paths() {
  for p in $SNAP_PATHS; do
    if [ -e "$p" ] || [ -L "$p" ]; then say "$p"; fi
  done
}

list_files() {
  for p in $(existing_paths); do
    "$FIND" "$p" ! -path '/opt/etc/xcp/backup/xcp.bak.*' \( -type f -o -type l \) -print
  done | LC_ALL=C sort
}

list_dirs() {
  for p in $(existing_paths); do
    "$FIND" "$p" ! -path '/opt/etc/xcp/backup/xcp.bak.*' -type d -print
  done | LC_ALL=C sort
}

hash_of() {
  if [ -L "$1" ]; then
    say "L:$(readlink "$1")"
  else
    md5sum "$1" 2>/dev/null | cut -d' ' -f1
  fi
}

# md5 (или L:<цель ссылки>) для путей со stdin: путь <TAB> хеш
md5_list() {
  while IFS= read -r p; do
    printf '%s\t%s\n' "$p" "$(hash_of "$p")"
  done
}

make_md5() {
  list_files | md5_list
}

# Сигнатура файлов (путь, размер, время изменения): дешёвая проверка, что файлы не
# менялись, пока шёл tar (md5 считается один раз, а не до и после).
make_sig() {
  for p in $(existing_paths); do
    "$FIND" "$p" ! -path '/opt/etc/xcp/backup/xcp.bak.*' \( -type f -o -type l \) -printf '%p\t%s\t%T@\n'
  done | LC_ALL=C sort
}

# сравнение текущего списка (файл $1) с базовым: строки CHANGED/EXTRA/MISSING <TAB> путь
diff_state() {
  awk -F'\t' '
    NR==FNR { b[$1]=$2; next }
    { c[$1]=$2
      if (!($1 in b)) print "EXTRA\t" $1
      else if (b[$1] != $2) print "CHANGED\t" $1 }
    END { for (k in b) if (!(k in c)) print "MISSING\t" k }
  ' "$BASE/md5.txt" "$1" | LC_ALL=C sort
}

# изменчивые файлы: меняются сами (ядро, панель, cron xkeen -ug), на код verify не влияют
is_volatile() {
  case "$1" in
    /opt/etc/xkeen/.last_policy_mark | /opt/etc/xkeen/ipset/* | /opt/etc/mihomo/cache.db) return 0 ;;
    /opt/etc/xray/*.dat | /opt/etc/xray/*.mmdb | /opt/etc/xray/*.metadb) return 0 ;;
    /opt/etc/mihomo/*.dat | /opt/etc/mihomo/*.mmdb | /opt/etc/mihomo/*.metadb) return 0 ;;
    /opt/etc/xray/configs/04_outbounds.sub_*.tail.json | /opt/etc/xray/configs/04_outbounds.zz_xcp_*) return 0 ;;
    /opt/etc/xcp/*) return 0 ;;
    # хук iptables XKeen перегенерируется при каждом старте ядра; на время записи рядом
    # лежит временный файл proxy.sh.tmp.<pid>
    /opt/etc/ndm/netfilter.d/proxy.sh | /opt/etc/ndm/netfilter.d/proxy.sh.tmp.*) return 0 ;;
  esac
  return 1
}

# разбор diff_state ($1) на $WORK/diff.nonvol и $WORK/diff.vol
split_diff() {
  : >"$WORK/diff.nonvol"
  : >"$WORK/diff.vol"
  diff_state "$1" | while IFS="$TAB" read -r kind path; do
    if is_volatile "$path"; then
      printf '%s\t%s\n' "$kind" "$path" >>"$WORK/diff.vol"
    else
      printf '%s\t%s\n' "$kind" "$path" >>"$WORK/diff.nonvol"
    fi
  done
}

# ---------- версии, ядро, xcp ----------

# Версия бинарника, только если это ELF: файл без заголовка не выполняем (на
# повреждённом бинарнике sh принял бы его за скрипт).
bin_ver() {
  b=$1
  shift
  [ -x "$b" ] || { say "нет"; return 0; }
  # od -c (без -An: в busybox его нет): первая строка «0000000 177 E L F»
  if [ "$(head -c 4 "$b" 2>/dev/null | od -c | sed -n '1s/^[0-9]* *//p' | tr -d ' ')" != '177ELF' ]; then
    say "не ELF"
    return 0
  fi
  "$b" "$@" 2>/dev/null | head -1
}

versions_now() {
  say "xray=$(bin_ver /opt/sbin/xray version)"
  say "mihomo=$(bin_ver /opt/sbin/mihomo -v)"
  say "xkeen=$(grep '^xkeen_current_version=' /opt/sbin/.xkeen/01_info/01_info_variable.sh 2>/dev/null | head -1 | cut -d'"' -f2)"
  say "xcp=$(/opt/sbin/xcp -v 2>/dev/null | head -1)"
}

active_core() {
  x=$(pidof xray 2>/dev/null | cut -d' ' -f1)
  m=$(pidof mihomo 2>/dev/null | cut -d' ' -f1)
  if [ -n "$x" ] && [ -n "$m" ]; then
    say both
  elif [ -n "$x" ]; then
    say xray
  elif [ -n "$m" ]; then
    say mihomo
  else
    say none
  fi
}

base_core() { grep '^core=' "$BASE/kernel.txt" | head -1 | cut -d= -f2; }

wait_core_gone() {
  i=0
  while [ -n "$(pidof xray 2>/dev/null)$(pidof mihomo 2>/dev/null)" ]; do
    i=$((i + 1))
    [ "$i" -ge 30 ] && return 1
    sleep 1
  done
  return 0
}

wait_core_up() {
  i=0
  while [ -z "$(pidof "$1" 2>/dev/null)" ]; do
    i=$((i + 1))
    [ "$i" -ge 120 ] && return 1
    sleep 1
  done
  return 0
}

# Ядро после xkeen -start должно продержаться с одним и тем же PID 30 с: XKeen на медленном
# устройстве поднимает процесс, заменяет его и только потом выходит на рабочее состояние.
# Если ядро исчезло и не вернулось за 40 с, запуск повторяется один раз.
wait_core_stable() {
  core=$1
  last=""
  same=0
  gone=0
  retried=0
  t=0
  while [ "$t" -lt 240 ]; do
    p=$(pidof "$core" 2>/dev/null | cut -d' ' -f1)
    if [ -n "$p" ]; then
      gone=0
      if [ "$p" = "$last" ]; then
        same=$((same + 5))
      else
        last=$p
        same=0
      fi
      [ "$same" -ge 30 ] && return 0
    else
      last=""
      same=0
      gone=$((gone + 5))
      if [ "$gone" -ge 40 ] && [ "$retried" = 0 ]; then
        say "ядро $core пропало после запуска — повторяю xkeen -start"
        "$XKEEN" -start >/dev/null 2>&1
        retried=1
      fi
    fi
    sleep 5
    t=$((t + 5))
  done
  return 1
}

# ---------- xcp ----------

stop_xcp() {
  if ! pidof xcp >/dev/null 2>&1; then
    say "xcp уже остановлен"
    return 0
  fi
  "$INITD_XCP" stop >/dev/null 2>&1
  i=0
  while pidof xcp >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -ge 15 ] && break
    sleep 1
  done
  if pidof xcp >/dev/null 2>&1; then
    kill $(pidof xcp) 2>/dev/null
    i=0
    while pidof xcp >/dev/null 2>&1; do
      i=$((i + 1))
      if [ "$i" -ge 15 ]; then
        kill -9 $(pidof xcp) 2>/dev/null
        sleep 1
        break
      fi
      sleep 1
    done
  fi
  if pidof xcp >/dev/null 2>&1; then
    say "ОШИБКА: xcp не остановился"
    return 1
  fi
  say "xcp остановлен"
}

start_xcp() {
  if pidof xcp >/dev/null 2>&1; then
    say "xcp уже запущен"
  else
    "$INITD_XCP" start >/dev/null 2>&1
  fi
  i=0
  while ! pidof xcp >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge 10 ]; then
      say "ОШИБКА: xcp не запустился"
      return 1
    fi
    sleep 1
  done
  # процесс уже есть, но панель ещё может не слушать порт: ждём ответа на loopback
  # (без ошибки: если curl или порта нет, смоук проверит панель сам)
  if command -v curl >/dev/null 2>&1; then
    i=0
    while ! curl -s -o /dev/null --max-time 2 http://127.0.0.1:8091/api/version 2>/dev/null; do
      i=$((i + 1))
      [ "$i" -ge 30 ] && break
      sleep 1
    done
  fi
  got=$(/opt/sbin/xcp -v 2>/dev/null | head -1)
  if [ -f "$BASE/versions.txt" ]; then
    want=$(grep '^xcp=' "$BASE/versions.txt" | head -1 | cut -d= -f2-)
    if [ "$got" != "$want" ]; then
      say "ОШИБКА: версия xcp $got, в снимке $want"
      return 1
    fi
  fi
  say "xcp запущен, версия $got"
}

# Бинарник из жёсткой ссылки снимка: копия рядом и переименование; содержимое ссылки
# сверяется с md5 снимка (запись «на месте» испортила бы и ссылку — тогда восстановить нечем).
restore_bin() {
  src="$BASE/bin/$(basename "$1")"
  want=$(grep "^$1$TAB" "$BASE/md5.txt" | head -1 | cut -f2)
  [ -f "$src" ] && [ "$(hash_of "$src")" = "$want" ] || return 1
  cp -p "$src" "$1.xcprt" && mv -f "$1.xcprt" "$1"
}

# ---------- подкоманды ----------

cmd_preflight() {
  # tar с режимом создания нужен только GNU: busybox tar создавать архивы не умеет,
  # и снимок без этой проверки дал бы ложный успех.
  if ! "$TAR" --version 2>/dev/null | grep -q GNU; then
    say "нужен GNU tar (opkg install tar): $TAR не GNU"
    return 5
  fi
  hhmm=$(date +%H%M)
  if [ "$hhmm" -ge 0520 ] && [ "$hhmm" -le 0545 ]; then
    say "время роутера $hhmm — окно cron xkeen -ug (05:30), подождите до 05:45"
    return 6
  fi
  if [ -f "$BASE/complete" ]; then
    say "остался снимок незавершённого прогона ($BASE): выполните restore или cleanup"
    return 7
  fi
  say "preflight: GNU tar есть, время роутера $(date +%H:%M), окно cron вне"
}

cmd_snapshot() {
  rm -rf "$BASE"
  mkdir -p "$BASE"
  chmod 700 "$BASE"
  xcp_was=no
  pidof xcp >/dev/null 2>&1 && xcp_was=yes
  if [ "$xcp_was" = yes ]; then
    stop_xcp || return 1
  fi

  # исключения tar: резервные копии самообновления xcp и бинарники, взятые жёсткой ссылкой
  mkdir -p "$BASE/bin"
  printf '%s\n' 'opt/etc/xcp/backup/xcp.bak.*' >"$WORK/tar.exclude"
  : >"$BASE/bin.list"
  for b in $BIN_PATHS; do
    [ -f "$b" ] && [ ! -L "$b" ] || continue
    if ln "$b" "$BASE/bin/$(basename "$b")" 2>/dev/null; then
      printf '%s\n' "$b" >>"$BASE/bin.list"
      printf '%s\n' "${b#/}" >>"$WORK/tar.exclude"
    fi
  done

  ok=0
  try=0
  while [ "$try" -lt 3 ]; do
    try=$((try + 1))
    make_sig >"$WORK/sig.before"
    list_dirs >"$BASE/dirs.txt"
    rm -f "$BASE/baseline.tar.gz"
    GZIP=-1 "$TAR" -C / -czpf "$BASE/baseline.tar.gz" -X "$WORK/tar.exclude" $(existing_paths | sed 's#^/##') 2>"$WORK/tar.err"
    if [ ! -s "$BASE/baseline.tar.gz" ]; then
      say "ОШИБКА: tar не создал baseline.tar.gz:"
      head -5 "$WORK/tar.err"
      rm -rf "$BASE"
      [ "$xcp_was" = yes ] && start_xcp
      return 1
    fi
    make_sig >"$WORK/sig.after"
    if cmp -s "$WORK/sig.before" "$WORK/sig.after"; then
      cut -f1 "$WORK/sig.after" >"$BASE/files.txt"
      md5_list <"$BASE/files.txt" >"$BASE/md5.txt"
      ok=1
      break
    fi
    say "файлы менялись во время снимка, попытка $try"
    sleep 2
  done
  if [ "$ok" != 1 ]; then
    say "ОШИБКА: не удалось снять стабильный снимок"
    rm -rf "$BASE"
    [ "$xcp_was" = yes ] && start_xcp
    return 1
  fi

  versions_now >"$BASE/versions.txt"
  {
    say "core=$(active_core)"
    say "xcp=$xcp_was"
    say "xkeen=$("$XKEEN" -status 2>/dev/null | strip_ansi | head -1 | tr -s ' ')"
  } >"$BASE/kernel.txt"
  chmod 600 "$BASE"/* 2>/dev/null
  : >"$BASE/complete"

  if [ "$xcp_was" = yes ]; then
    start_xcp || return 1
  fi
  say "снимок: файлов $(wc -l <"$BASE/files.txt"), каталогов $(wc -l <"$BASE/dirs.txt"), tar $(du -k "$BASE/baseline.tar.gz" | cut -f1) КБ"
  cat "$BASE/versions.txt" "$BASE/kernel.txt"
}

cmd_restore() {
  [ -f "$BASE/complete" ] || { say "ОШИБКА: нет завершённого снимка"; return 4; }
  n=$(wc -l <"$BASE/files.txt" 2>/dev/null || echo 0)
  [ "$n" -ge 10 ] || { say "ОШИБКА: files.txt слишком короткий ($n строк)"; return 4; }
  [ -f "$BASE/baseline.tar.gz" ] || { say "ОШИБКА: нет baseline.tar.gz"; return 4; }

  want_core=$(base_core)
  want_xcp=no
  grep -q '^xcp=yes' "$BASE/kernel.txt" && want_xcp=yes

  # панель пишет в свои файлы и перезапускает ядро — останавливаем её первой
  stop_xcp || return 4

  make_md5 >"$WORK/md5.now"
  diffs=$(diff_state "$WORK/md5.now" | wc -l)
  if [ "$diffs" = 0 ] && [ "$(active_core)" = "$want_core" ]; then
    say "restore: изменений нет, ядро $want_core — ядро не трогаю"
  else
    say "restore: расхождений файлов $diffs, ядро сейчас $(active_core), нужно $want_core"
    "$XKEEN" -stop >/dev/null 2>&1
    if ! wait_core_gone; then
      say "ОШИБКА: ядро не остановилось после xkeen -stop (kill по pidof не применяю)"
      return 4
    fi
    make_md5 >"$WORK/md5.now"

    # лишние файлы — только внутри SNAP_PATHS (список построен по ним), только вне files.txt
    # и только под /opt
    diff_state "$WORK/md5.now" | while IFS="$TAB" read -r kind path; do
      [ "$kind" = EXTRA ] || continue
      case "$path" in /opt/*) rm -f "$path" ;; esac
    done
    list_dirs | grep -vxFf "$BASE/dirs.txt" | LC_ALL=C sort -r | while IFS= read -r d; do
      case "$d" in /opt/*) rmdir "$d" 2>/dev/null ;; esac
    done

    # из архива берём только изменённые и пропавшие файлы и пропавшие каталоги: запись
    # всего архива на диск роутера занимает минуты, а остальное и так равно снимку
    : >"$WORK/restore.list"
    diff_state "$WORK/md5.now" | while IFS="$TAB" read -r kind path; do
      case "$kind" in
        CHANGED | MISSING)
          if grep -qxF "$path" "$BASE/bin.list" 2>/dev/null; then
            restore_bin "$path" || say "ОШИБКА: бинарник $path не восстановлен"
          else
            printf '%s\n' "${path#/}" >>"$WORK/restore.list"
          fi
          ;;
      esac
    done
    while IFS= read -r d; do
      [ -d "$d" ] || printf '%s\n' "${d#/}"
    done <"$BASE/dirs.txt" >>"$WORK/restore.list"
    if [ -s "$WORK/restore.list" ]; then
      say "restore: из архива извлекаю $(wc -l <"$WORK/restore.list") путей"
      "$TAR" -C / -xpzf "$BASE/baseline.tar.gz" -T "$WORK/restore.list" 2>"$WORK/untar.err" || {
        say "ОШИБКА: распаковка tar не удалась"
        head -5 "$WORK/untar.err"
        return 4
      }
    fi

    make_md5 >"$WORK/md5.now"
    left=$(diff_state "$WORK/md5.now" | wc -l)
    if [ "$left" != 0 ]; then
      say "ОШИБКА: после восстановления расхождений: $left (ядро не запускаю)"
      diff_state "$WORK/md5.now" | head -20
      return 4
    fi
    left_dirs=$(list_dirs | grep -cvxFf "$BASE/dirs.txt")
    [ "$left_dirs" = 0 ] || { say "ОШИБКА: остались лишние каталоги: $left_dirs"; return 4; }
    say "restore: файлы совпадают со снимком"

    case "$want_core" in
      xray | mihomo)
        "$XKEEN" -start >/dev/null 2>&1
        if ! wait_core_up "$want_core"; then
          say "ОШИБКА: ядро $want_core не поднялось за 120 с"
          return 4
        fi
        if ! wait_core_stable "$want_core"; then
          say "ОШИБКА: ядро $want_core не держится после запуска"
          return 4
        fi
        say "restore: ядро $want_core запущено"
        ;;
      *) say "restore: в снимке ядро не было запущено — не запускаю" ;;
    esac
  fi

  if [ "$want_xcp" = yes ]; then
    start_xcp || return 4
  fi
}

cmd_verify() {
  [ -f "$BASE/complete" ] || { say "ОШИБКА: нет завершённого снимка"; return 1; }
  rc=0
  make_md5 >"$WORK/md5.now"
  split_diff "$WORK/md5.now"
  if [ -s "$WORK/diff.nonvol" ]; then
    say "ОШИБКА: неизменчивые файлы отличаются от снимка:"
    head -30 "$WORK/diff.nonvol"
    rc=1
  else
    say "md5 неизменчивых файлов: совпадают, лишних и пропавших нет"
  fi
  versions_now >"$WORK/ver.now"
  if cmp -s "$BASE/versions.txt" "$WORK/ver.now"; then
    say "версии: совпадают"
  else
    say "ОШИБКА: версии отличаются:"
    diff "$BASE/versions.txt" "$WORK/ver.now"
    rc=1
  fi
  if [ "$(active_core)" = "$(base_core)" ]; then
    say "активное ядро: $(active_core) — совпадает"
  else
    say "ОШИБКА: активное ядро $(active_core), в снимке $(base_core)"
    rc=1
  fi
  if grep -q '^xcp=yes' "$BASE/kernel.txt"; then
    if pidof xcp >/dev/null 2>&1; then
      want=$(grep '^xcp=' "$BASE/versions.txt" | head -1 | cut -d= -f2-)
      got=$(/opt/sbin/xcp -v 2>/dev/null | head -1)
      if [ "$got" = "$want" ]; then
        say "xcp: запущен, версия $got"
      else
        say "ОШИБКА: версия xcp $got, в снимке $want"
        rc=1
      fi
    else
      say "ОШИБКА: xcp не запущен"
      rc=1
    fi
  fi
  if [ -s "$WORK/diff.vol" ]; then
    say "изменены после запуска (изменчивые, на код не влияют): $(wc -l <"$WORK/diff.vol")"
  fi
  say "verify: код $rc"
  rm -f "$WORK/md5.now" "$WORK/diff.nonvol" "$WORK/diff.vol" "$WORK/ver.now"
  return $rc
}

cmd_cleanup() {
  rm -rf "$BASE" "$WORK"
  say "снимок удалён"
}

cmd_status() {
  versions_now
  say "core=$(active_core)"
  say "xcp_pid=$(pidof xcp 2>/dev/null)"
  say "time=$(date +%H:%M:%S)"
  if [ -f "$BASE/complete" ]; then
    say "baseline=есть ($(wc -l <"$BASE/files.txt") файлов)"
  else
    say "baseline=нет"
  fi
}

cmd=${1:-}
case "$cmd" in
  preflight) cmd_preflight ;;
  snapshot) cmd_snapshot ;;
  restore) cmd_restore ;;
  verify) cmd_verify ;;
  cleanup) cmd_cleanup ;;
  status) cmd_status ;;
  *)
    say "usage: snapshot.sh preflight|snapshot|restore|verify|cleanup|status"
    exit 2
    ;;
esac
rc=$?
# пустой рабочий каталог после команды не оставляем (cleanup удаляет его целиком)
rmdir "$WORK" 2>/dev/null
exit $rc
