#!/usr/bin/env bash
#
# verify-tproxy-disarm.sh — Воспроизводимая сверка аварийного разоружения TPROXY на роутере.
# Скрипт фиксирует состояние mangle до остановки ядра, останавливает ядро, дожидается
# срабатывания watchdog, снимает mangle после, строит diff и сохраняет окно xcp.log.
#
# Использование:
#   scripts/verify-tproxy-disarm.sh [--out DIR] [--wait SEC] [--no-restore] [--dry-run]
#

set -euo pipefail

SSH_ALIAS="router-shi"
OUT_DIR=""
WAIT_SEC=240
RESTORE=true
DRY_RUN=false

usage() {
    cat <<'EOF'
Использование: scripts/verify-tproxy-disarm.sh [опции]

Опции:
  --out DIR       Каталог для сохранения артефактов (по умолчанию создается временный)
  --wait SEC      Максимальное время ожидания срабатывания watchdog в секундах (по умолчанию 240)
  --no-restore    Не восстанавливать (не запускать) ядро после прогона
  --dry-run       Вывести план команд без обращения к роутеру и выйти с кодом 0
  -h, --help      Показать эту справку
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --out)
            OUT_DIR="$2"
            shift 2
            ;;
        --wait)
            WAIT_SEC="$2"
            shift 2
            ;;
        --no-restore)
            RESTORE=false
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Неизвестный параметр: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if [[ "$DRY_RUN" = true ]]; then
    echo "=== DRY RUN: План проверки аварийного разоружения TPROXY ==="
    echo "Целевой хост: $SSH_ALIAS"
    echo "Таймаут ожидания: $WAIT_SEC сек"
    echo "Восстановление ядра: $RESTORE"
    echo "План выполнения:"
    echo "  1. Проверка доступности SSH ($SSH_ALIAS)"
    echo "  2. Сбор версий: ssh $SSH_ALIAS 'iptables --version; ip6tables --version'"
    echo "  3. Проверка активности процесса xcp: ssh $SSH_ALIAS 'pidof xcp'"
    echo "  4. Снимок mangle до: ssh $SSH_ALIAS 'iptables-save -t mangle; ip6tables-save -t mangle'"
    echo "  5. Подсчет правил перехвата (-j TPROXY или маркер XKEEN_TPROXY). Если 0 -> выход 2"
    echo "  6. Замер смещения в /opt/var/log/xcp.log"
    echo "  7. Остановка ядра: ssh $SSH_ALIAS '/opt/sbin/xkeen -stop'"
    echo "  8. Опрос iptables-save -t mangle каждые 10 сек (до $WAIT_SEC сек)"
    echo "  9. Снимок mangle после, построение diff"
    echo " 10. Извлечение строк с префиксом Watchdog: из /opt/var/log/xcp.log"
    echo " 11. Вердикт: DISARM VERIFIED (код 0) или DISARM NOT VERIFIED (код 1)"
    echo " 12. В trap EXIT: восстановление ядра (ssh $SSH_ALIAS '/opt/sbin/xkeen -start')"
    exit 0
fi

if [[ -z "$OUT_DIR" ]]; then
    OUT_DIR="$(mktemp -d -t xcp-disarm-XXXXXX)"
else
    mkdir -p "$OUT_DIR"
fi

echo "Каталог артефактов: $OUT_DIR"

# 1. Preflight
echo "[1/7] Проверка доступности роутера ($SSH_ALIAS)..."
if ! ssh -q "$SSH_ALIAS" true; then
    echo "ОШИБКА: Роутер ($SSH_ALIAS) недоступен по SSH" >&2
    exit 2
fi

echo "[1/7] Сбор версий iptables..."
ssh "$SSH_ALIAS" "iptables --version 2>&1; ip6tables --version 2>&1 || true" > "$OUT_DIR/iptables-version.txt"
cat "$OUT_DIR/iptables-version.txt"

echo "[1/7] Проверка процесса панели xcp..."
if ! ssh "$SSH_ALIAS" "pidof xcp" >/dev/null 2>&1; then
    echo "ОШИБКА: Процесс xcp не запущен на $SSH_ALIAS" >&2
    exit 2
fi

# Определение пути к xkeen на роутере
XKEEN_BIN=$(ssh "$SSH_ALIAS" 'command -v xkeen 2>/dev/null || { [ -x /opt/sbin/xkeen ] && echo /opt/sbin/xkeen; } || echo xkeen')

# 2. Снимок «до»
echo "[2/7] Снятие снимка mangle ДО отключения ядра..."
ssh "$SSH_ALIAS" "iptables-save -t mangle 2>&1; echo '--- IP6TABLES ---'; ip6tables-save -t mangle 2>&1 || true" > "$OUT_DIR/mangle-before.txt"

RULES_BEFORE=$(grep -E '\-j TPROXY|XKEEN_TPROXY' "$OUT_DIR/mangle-before.txt" | wc -l || true)
echo "Обнаружено активных правил перехвата TPROXY: $RULES_BEFORE"

if [[ "$RULES_BEFORE" -eq 0 ]]; then
    echo "ПРЕДУПРЕЖДЕНИЕ: В таблице mangle нет правил перехвата TPROXY." >&2
    echo "Сценарий проверки неприменим, так как снимать нечего." >&2
    exit 2
fi

# 3. Отметка смещения в логе
echo "[3/7] Фиксация текущей позиции в xcp.log..."
LOG_LINES_BEFORE=$(ssh "$SSH_ALIAS" "wc -l < /opt/var/log/xcp.log 2>/dev/null || echo 0")

# Установка trap на восстановление ядра при любом выходе
restore_kernel() {
    local exit_code=$?
    if [[ "$RESTORE" = true ]]; then
        echo "Восстановление ядра на $SSH_ALIAS..."
        ssh "$SSH_ALIAS" "$XKEEN_BIN -start" >/dev/null 2>&1 || true
    fi
    exit $exit_code
}
trap restore_kernel EXIT

# 4. Триггер — остановка ядра
echo "[4/7] Остановка ядра XKeen ($XKEEN_BIN -stop)..."
ssh "$SSH_ALIAS" "$XKEEN_BIN -stop" >/dev/null 2>&1 || true

# 5. Ожидание срабатывания watchdog
echo "[5/7] Ожидание срабатывания watchdog (до $WAIT_SEC сек)..."
START_TIME=$(date +%s)
DISARMED=false

while true; do
    CURRENT_RULES=$(ssh "$SSH_ALIAS" "iptables-save -t mangle 2>&1" | grep -E '\-j TPROXY|XKEEN_TPROXY' | wc -l || true)
    if [[ "$CURRENT_RULES" -eq 0 ]]; then
        DISARMED=true
        echo "Правила TPROXY исчезли из iptables mangle!"
        break
    fi

    NOW=$(date +%s)
    ELAPSED=$((NOW - START_TIME))
    if [[ $ELAPSED -ge $WAIT_SEC ]]; then
        echo "Таймаут ожидания ($WAIT_SEC сек) истек, осталось правил: $CURRENT_RULES"
        break
    fi

    echo "Прошло $ELAPSED сек, правил перехвата: $CURRENT_RULES. Ожидание..."
    sleep 10
done

# 6. Снимок «после» и построение diff
echo "[6/7] Снятие снимка mangle ПОСЛЕ прогона..."
ssh "$SSH_ALIAS" "iptables-save -t mangle 2>&1; echo '--- IP6TABLES ---'; ip6tables-save -t mangle 2>&1 || true" > "$OUT_DIR/mangle-after.txt"

diff -u "$OUT_DIR/mangle-before.txt" "$OUT_DIR/mangle-after.txt" > "$OUT_DIR/mangle.diff" || true

# 7. Извлечение окна лога
echo "[7/7] Извлечение записей Watchdog из xcp.log..."
ssh "$SSH_ALIAS" "tail -n +$((LOG_LINES_BEFORE + 1)) /opt/var/log/xcp.log 2>/dev/null || true" | grep 'Watchdog:' > "$OUT_DIR/xcp-log-window.txt" || true

RULES_AFTER=$(grep -E '\-j TPROXY|XKEEN_TPROXY' "$OUT_DIR/mangle-after.txt" | wc -l || true)

echo "=== ИТОГИ ПРОВЕРКИ ==="
cat "$OUT_DIR/xcp-log-window.txt" || true
echo "----------------------"

if [[ "$DISARMED" = true ]] && [[ "$RULES_AFTER" -eq 0 ]]; then
    echo "DISARM VERIFIED: снято правил: $RULES_BEFORE, осталось: 0. Артефакты сохранены в $OUT_DIR"
    echo "$OUT_DIR"
    exit 0
else
    echo "DISARM NOT VERIFIED: снято правил: $((RULES_BEFORE - RULES_AFTER)), осталось: $RULES_AFTER. Артефакты сохранены в $OUT_DIR"
    echo "$OUT_DIR"
    exit 1
fi
