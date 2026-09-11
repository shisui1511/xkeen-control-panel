#!/usr/bin/env bash
#
# measure-xcp-log-rate.sh — Воспроизводимый замер темпа прироста xcp.log на роутере.
# Скрипт фиксирует исходное состояние лога, штатно останавливает ядро через xkeen,
# выдерживает заданное окно замера, снимает итоговый прирост строк, проверяет
# отсутствие ротации и восстанавливает ядро в завершающем обработчике.
#
# Использование:
#   scripts/measure-xcp-log-rate.sh [--window MIN] [--out DIR] [--no-restore] [--dry-run]
#

set -euo pipefail

SSH_ALIAS="router-shi"
OUT_DIR=""
WINDOW_MIN=60
RESTORE=true
DRY_RUN=false

SSH_OPTS=(-o ConnectTimeout=10 -o BatchMode=yes)

usage() {
    cat <<'EOF'
Использование: scripts/measure-xcp-log-rate.sh [опции]

Опции:
  --window MIN    Длительность окна замера в минутах (по умолчанию 60, минимум 3)
  --out DIR       Каталог для сохранения артефактов (по умолчанию создается временный)
  --no-restore    Не запускать ядро обратно после замера
  --dry-run       Вывести план команд без обращения к роутеру и выйти с кодом 0
  -h, --help      Показать эту справку
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --window)
            [[ $# -ge 2 ]] || { echo "ОШИБКА: отсутствует значение для параметра --window" >&2; exit 2; }
            if ! [[ "$2" =~ ^[0-9]+$ ]]; then
                echo "ОШИБКА: параметр --window должен быть целым положительным числом (минуты): $2" >&2
                exit 2
            fi
            WINDOW_MIN="$2"
            shift 2
            ;;
        --out)
            [[ $# -ge 2 ]] || { echo "ОШИБКА: отсутствует значение для параметра --out" >&2; exit 2; }
            OUT_DIR="$2"
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

if [[ "$WINDOW_MIN" -lt 3 ]]; then
    echo "ОШИБКА: длительность окна замера должна быть не менее 3 минут (получено: $WINDOW_MIN)" >&2
    exit 2
fi

if [[ "$DRY_RUN" = true ]]; then
    echo "=== DRY RUN: План замера прироста xcp.log на роутере ==="
    echo "Целевой хост: $SSH_ALIAS"
    echo "Окно замера: $WINDOW_MIN мин"
    echo "Восстановление ядра: $RESTORE"
    echo "План выполнения:"
    echo "  1. Проверка доступности SSH ($SSH_ALIAS)"
    echo "  2. Проверка процесса панели: ssh $SSH_ALIAS 'pidof xcp'"
    echo "  3. Проверка наличия /opt/var/log/xcp.log"
    echo "  4. Проверка состояния ядра до старта: ssh $SSH_ALIAS 'pidof mihomo xray'. Если пусто -> выход 2"
    echo "  5. Фиксация начального состояния лога: wc -l, размер, список файлов ротации"
    echo "  6. Установка trap EXIT на восстановление ядра: ssh $SSH_ALIAS 'xkeen -start'"
    echo "  7. Остановка ядра: ssh $SSH_ALIAS 'xkeen -stop'"
    echo "  8. Ожидание $WINDOW_MIN мин с периодической фиксацией промежуточных отсчетов"
    echo "  9. Снятие итогового состояния: wc -l, размер, список файлов ротации, выборка строк Watchdog и dedup"
    echo " 10. Расчет суточного темпа: delta * 1440 / $WINDOW_MIN строк/сутки"
    echo " 11. Вердикт: успех (код 0) если темп <= 100 строк/сутки и нет ротации, иначе код 1"
    echo " 12. В trap EXIT: запуск ядра обратно"
    exit 0
fi

if [[ -z "$OUT_DIR" ]]; then
    OUT_DIR="$(mktemp -d -t xcp-log-rate-XXXXXX)"
else
    mkdir -p "$OUT_DIR"
fi

echo "Каталог артефактов: $OUT_DIR"
echo "Параметры замера: окно=$WINDOW_MIN мин, хост=$SSH_ALIAS"

# 1. Preflight
echo "[1/6] Проверка доступности роутера ($SSH_ALIAS)..."
if ! ssh "${SSH_OPTS[@]}" -q "$SSH_ALIAS" true; then
    echo "ОШИБКА: Роутер ($SSH_ALIAS) недоступен по SSH" >&2
    exit 2
fi

echo "[1/6] Проверка процесса панели xcp..."
if ! ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "pidof xcp" >/dev/null 2>&1; then
    echo "ОШИБКА: Процесс панели xcp не запущен на $SSH_ALIAS" >&2
    exit 2
fi

echo "[1/6] Проверка наличия файла лога..."
if ! ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "[ -f /opt/var/log/xcp.log ]"; then
    echo "ОШИБКА: Файл /opt/var/log/xcp.log отсутствует на $SSH_ALIAS" >&2
    exit 2
fi

XKEEN_BIN=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" 'command -v xkeen 2>/dev/null || { [ -x /opt/sbin/xkeen ] && echo /opt/sbin/xkeen; } || echo xkeen')

echo "[1/6] Проверка состояния ядра перед стартом..."
KERNEL_PIDS_BEFORE=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "pidof mihomo xray 2>/dev/null || true")
if [[ -z "$KERNEL_PIDS_BEFORE" ]]; then
    echo "ОШИБКА: Ядро (mihomo/xray) уже остановлено до старта замера. Замер требует контролируемой остановки." >&2
    exit 2
fi
echo "Ядро активно (PID: $KERNEL_PIDS_BEFORE)"

# 2. Исходный снимок
echo "[2/6] Снятие исходного состояния xcp.log..."
LINES_START=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "wc -l < /opt/var/log/xcp.log")
BYTES_START=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "wc -c < /opt/var/log/xcp.log")
ROTATION_BEFORE=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "ls -la /opt/var/log/xcp.log* 2>/dev/null || true")
echo "$ROTATION_BEFORE" > "$OUT_DIR/rotation-before.txt"

echo "Начальные строки: $LINES_START" | tee "$OUT_DIR/initial-lines.txt"
echo "Начальный размер: $BYTES_START байт"
cat "$OUT_DIR/rotation-before.txt"

# 3. Установка trap на восстановление ядра
restore_kernel() {
    local exit_code=$?
    if [[ "$RESTORE" = true ]]; then
        echo "Восстановление (запуск) ядра на $SSH_ALIAS..."
        ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "$XKEEN_BIN -start" >/dev/null 2>&1 || true
        sleep 2
        local pids
        pids=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "pidof mihomo xray 2>/dev/null || true")
        if [[ -n "$pids" ]]; then
            echo "Ядро успешно восстановлено (PID: $pids)"
        else
            echo "ПРЕДУПРЕЖДЕНИЕ: Не удалось подтвердить запуск ядра после замера" >&2
        fi
    fi
    exit $exit_code
}
trap restore_kernel EXIT INT TERM

# 4. Остановка ядра
echo "[3/6] Штатная остановка ядра через $XKEEN_BIN -stop..."
ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "$XKEEN_BIN -stop" > "$OUT_DIR/stop-output.txt" 2>&1 || true
sleep 3
KERNEL_PIDS_STOPPED=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "pidof mihomo xray 2>/dev/null || true")
if [[ -n "$KERNEL_PIDS_STOPPED" ]]; then
    echo "ПРЕДУПРЕЖДЕНИЕ: Процесс ядра все еще виден после stop: $KERNEL_PIDS_STOPPED" >&2
else
    echo "Ядро успешно остановлено"
fi

# 5. Окно ожидания с промежуточными отсчетами
TOTAL_SEC=$((WINDOW_MIN * 60))
STEP_SEC=30
if [[ "$WINDOW_MIN" -ge 30 ]]; then
    STEP_SEC=300
elif [[ "$WINDOW_MIN" -ge 10 ]]; then
    STEP_SEC=60
fi

echo "[4/6] Ожидание окна замера ($WINDOW_MIN мин = $TOTAL_SEC сек, шаг опроса: $STEP_SEC сек)..."
ELAPSED=0
START_TS=$(date +%s)
echo "timestamp,elapsed_sec,current_lines,delta_lines" > "$OUT_DIR/progress.csv"

while [[ $ELAPSED -lt $TOTAL_SEC ]]; do
    SLEEP_CHUNK=$STEP_SEC
    REMAINING=$((TOTAL_SEC - ELAPSED))
    if [[ $SLEEP_CHUNK -gt $REMAINING ]]; then
        SLEEP_CHUNK=$REMAINING
    fi
    sleep "$SLEEP_CHUNK"
    NOW_TS=$(date +%s)
    ELAPSED=$((NOW_TS - START_TS))
    CUR_LINES=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "wc -l < /opt/var/log/xcp.log" 2>/dev/null || echo "$LINES_START")
    DELTA_INTERMEDIATE=$((CUR_LINES - LINES_START))
    echo "$(date '+%Y-%m-%d %H:%M:%S'),$ELAPSED,$CUR_LINES,$DELTA_INTERMEDIATE" >> "$OUT_DIR/progress.csv"
    printf "  [+%-4d сек] текущие строки: %d (прирост: %+d)\n" "$ELAPSED" "$CUR_LINES" "$DELTA_INTERMEDIATE"
done

# 6. Финальный снимок и анализ
echo "[5/6] Снятие итогового состояния..."
ACTUAL_SEC=$(( $(date +%s) - START_TS ))
ACTUAL_MIN_FLOAT=$(awk "BEGIN {printf \"%.2f\", $ACTUAL_SEC / 60}")
LINES_END=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "wc -l < /opt/var/log/xcp.log")
BYTES_END=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "wc -c < /opt/var/log/xcp.log")
ROTATION_AFTER=$(ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "ls -la /opt/var/log/xcp.log* 2>/dev/null || true")
echo "$ROTATION_AFTER" > "$OUT_DIR/rotation-after.txt"

DELTA_LINES=$((LINES_END - LINES_START))
DELTA_BYTES=$((BYTES_END - BYTES_START))

# Извлечение добавленных строк лога
if [[ "$DELTA_LINES" -gt 0 ]]; then
    ssh "${SSH_OPTS[@]}" "$SSH_ALIAS" "tail -n $DELTA_LINES /opt/var/log/xcp.log" > "$OUT_DIR/new-log-lines.txt" 2>&1 || true
else
    touch "$OUT_DIR/new-log-lines.txt"
fi

grep -E 'Watchdog:' "$OUT_DIR/new-log-lines.txt" > "$OUT_DIR/watchdog-lines.txt" || true
grep -E '\[dedup\]' "$OUT_DIR/new-log-lines.txt" > "$OUT_DIR/dedup-lines.txt" || true

# Расчет пересчета на сутки (1440 минут)
DAILY_RATE=$(awk "BEGIN {printf \"%.1f\", ($DELTA_LINES * 86400) / $ACTUAL_SEC}")
DAILY_RATE_INT=$(awk "BEGIN {printf \"%d\", ($DELTA_LINES * 86400) / $ACTUAL_SEC}")

# Проверка файлов ротации
ROTATION_FAILED=false
ROT_FILES_BEFORE=$(grep -oE 'xcp\.log\.[0-9]+' "$OUT_DIR/rotation-before.txt" | sort | tr '\n' ' ' || true)
ROT_FILES_AFTER=$(grep -oE 'xcp\.log\.[0-9]+' "$OUT_DIR/rotation-after.txt" | sort | tr '\n' ' ' || true)

if [[ "$ROT_FILES_BEFORE" != "$ROT_FILES_AFTER" ]]; then
    ROTATION_FAILED=true
fi

CRITERIA_MET=true
if [[ "$DAILY_RATE_INT" -gt 100 ]] || [[ "$ROTATION_FAILED" = true ]]; then
    CRITERIA_MET=false
fi

# Формирование отчета
{
    echo "================================================================================"
    echo "ОТЧЕТ О ЗАМЕРЕ ПРИРОСТА XCP.LOG ПРИ ОСТАНОВЛЕННОМ ЯДРЕ (WD-03, WD-04)"
    echo "================================================================================"
    echo "Целевой хост: $SSH_ALIAS"
    echo "Длительность замера: $ACTUAL_SEC сек ($ACTUAL_MIN_FLOAT мин)"
    echo "Строки лога: начало=$LINES_START, конец=$LINES_END, прирост=+$DELTA_LINES строк"
    echo "Размер лога: начало=$BYTES_START байт, конец=$BYTES_END байт, прирост=+$DELTA_BYTES байт"
    echo "Суточный темп (экстраполяция 24ч): $DAILY_RATE строк/сутки (порог: <= 100)"
    echo "Ротация логов за время замера: $(if [[ "$ROTATION_FAILED" = true ]]; then echo "ОБНАРУЖЕНА (ПРОВАЛ)"; else echo "отсутствует (норма)"; fi)"
    echo "Файлы ротации до:   ${ROT_FILES_BEFORE:-нет}"
    echo "Файлы ротации после: ${ROT_FILES_AFTER:-нет}"
    echo "Строк с префиксом Watchdog:: $(wc -l < "$OUT_DIR/watchdog-lines.txt" | tr -d ' ')"
    echo "Строк сводок [dedup]:        $(wc -l < "$OUT_DIR/dedup-lines.txt" | tr -d ' ')"
    echo "--------------------------------------------------------------------------------"
    if [[ "$CRITERIA_MET" = true ]]; then
        echo "ВЕРДИКТ: КРИТЕРИЙ СОБЛЮДЕН (SUCCESS)"
    else
        echo "ВЕРДИКТ: КРИТЕРИЙ НЕ СОБЛЮДЕН (FAILURE)"
    fi
    echo "================================================================================"
    echo ""
    echo "--- Выборка строк Watchdog: ---"
    head -n 20 "$OUT_DIR/watchdog-lines.txt" || true
    echo ""
    echo "--- Выборка строк [dedup]: ---"
    head -n 20 "$OUT_DIR/dedup-lines.txt" || true
    echo ""
    echo "--- Промежуточная динамика ---"
    cat "$OUT_DIR/progress.csv"
} | tee "$OUT_DIR/report.txt"

echo "[6/6] Завершение замера. Артефакты сохранены в: $OUT_DIR"

if [[ "$CRITERIA_MET" = true ]]; then
    exit 0
else
    exit 1
fi
