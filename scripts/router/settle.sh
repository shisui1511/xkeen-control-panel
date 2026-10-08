#!/bin/sh
# Выполняется НА УСТРОЙСТВЕ (run.sh отправляет его через `sh -s`): ждёт, пока XKeen сообщит
# «запущен в режиме …» и в iptables появятся правила XKeen (TPROXY в mangle или REDIRECT в nat).
# Печатает ready и выходит с 0; не дождался за 600 с — timeout и код 1.
# Нужен после разрушающих Go-тестов (остановка, запуск, перезапуск XKeen): на медленном
# устройстве перехват и статус приходят в норму не сразу.
E=$(printf '\033')
i=0
while [ "$i" -lt 600 ]; do
  st=$(/opt/sbin/xkeen -status 2>&1 | sed "s/$E\[[0-9;]*[A-Za-z]//g")
  # только -w без числа: iptables 1.4.21 не принимает -w 5
  t=$(iptables -w -t mangle -S 2>/dev/null | grep -c "xkeen.*-j TPROXY")
  r=$(iptables -w -t nat -S 2>/dev/null | grep -c "xkeen.*-j REDIRECT")
  case "$st" in
    *"запущен в режиме"*)
      if [ $((t + r)) -gt 0 ]; then
        echo ready
        exit 0
      fi
      ;;
  esac
  i=$((i + 5))
  sleep 5
done
echo timeout
exit 1
