#!/bin/sh
# Выполняется НА УСТРОЙСТВЕ (run.sh отправляет его через `sh -s [stable]`): печатает состояние
# автозапуска строками ключ=значение. Только чтение.
#   boot_id   идентификатор текущей загрузки (меняется после перезагрузки)
#   xcp       версия панели (/opt/sbin/xcp -v)
#   xcp_pid   PID панели (пусто — не запущена)
#   xkeen     1 — XKeen сообщает «запущен в режиме …», иначе 0
#   core      xray | mihomo | both | none — какое ядро запущено
#   mangle    число правил с xkeen в `iptables -w -t mangle -S`
#   nat       число правил с xkeen в `iptables -w -t nat -S`
# С аргументом stable ядро перед снятием состояния должно продержаться с одним и тем же PID
# 30 с (после холодного старта XKeen поднимает ядро и на медленном устройстве заменяет его).
# Только -w без числа: iptables 1.4.21 не принимает -w 5.

E=$(printf '\033')

core_now() {
  x=$(pidof xray 2>/dev/null | cut -d' ' -f1)
  m=$(pidof mihomo 2>/dev/null | cut -d' ' -f1)
  if [ -n "$x" ] && [ -n "$m" ]; then
    echo "both:$x:$m"
  elif [ -n "$x" ]; then
    echo "xray:$x"
  elif [ -n "$m" ]; then
    echo "mihomo:$m"
  else
    echo "none:"
  fi
}

if [ "${1:-}" = stable ]; then
  prev=$(core_now)
  same=0
  i=0
  # до 180 с: нужно 30 с подряд с неизменным PID ядра
  while [ "$i" -lt 180 ] && [ "$same" -lt 30 ]; do
    sleep 5
    i=$((i + 5))
    cur=$(core_now)
    if [ "$cur" = "$prev" ]; then
      same=$((same + 5))
    else
      same=0
      prev=$cur
    fi
  done
fi

echo "boot_id=$(cat /proc/sys/kernel/random/boot_id 2>/dev/null)"
echo "xcp=$(/opt/sbin/xcp -v 2>/dev/null | head -n 1)"
echo "xcp_pid=$(pidof xcp 2>/dev/null | cut -d' ' -f1)"
st=$(/opt/sbin/xkeen -status 2>&1 | sed "s/$E\[[0-9;]*[A-Za-z]//g")
case "$st" in
  *"запущен в режиме"*) echo "xkeen=1" ;;
  *) echo "xkeen=0" ;;
esac
core_now | sed 's/^\([a-z]*\):.*/core=\1/'
echo "mangle=$(iptables -w -t mangle -S 2>/dev/null | grep -c xkeen)"
echo "nat=$(iptables -w -t nat -S 2>/dev/null | grep -c xkeen)"
