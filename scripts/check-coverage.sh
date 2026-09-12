#!/usr/bin/env bash
# scripts/check-coverage.sh — Проверка порогов тестового покрытия бэкенда.
# Использование: ./scripts/check-coverage.sh [path/to/coverage.out]

set -euo pipefail

COV_FILE="${1:-coverage.out}"

if [ ! -f "$COV_FILE" ]; then
    echo "Error: Coverage profile not found: $COV_FILE" >&2
    exit 1
fi

# Целевые пакеты и их пороги в процентах
# Формат: "путь_к_пакету:минимальный_порог"
TARGET_PACKAGES=(
    "internal/server:60.0"
    "internal/i18n:80.0"
    "internal/handlers:70.0"
    "internal/utils:70.0"
    "internal/auth:70.0"
)
TOTAL_THRESHOLD=70.0

FAILED=0

echo ""
printf "%-25s %10s   %10s   %8s\n" "Package" "Coverage" "Threshold" "Status"
echo "-------------------------------------------------------------"

# Проверка индивидуальных пакетов
for item in "${TARGET_PACKAGES[@]}"; do
    pkg="${item%%:*}"
    threshold="${item##*:}"

    stats=$(awk -v target="$pkg" '
    NR>1 {
        line = $1
        sub(/:.*$/, "", line)
        sub(/^github\.com\/[^\/]+\/[^\/]+\//, "", line)
        sub(/\/[^\/]+$/, "", line)
        if (line == target) {
            total += $(NF-1)
            if ($NF > 0) covered += $(NF-1)
        }
    }
    END {
        if (total > 0) {
            printf "%.1f %d %d", (covered / total) * 100, covered, total
        } else {
            printf "0.0 0 0"
        }
    }' "$COV_FILE")

    actual=$(echo "$stats" | awk '{print $1}')
    covered=$(echo "$stats" | awk '{print $2}')
    total=$(echo "$stats" | awk '{print $3}')

    # Сравнение чисел с плавающей точкой через awk
    passed=$(awk -v act="$actual" -v thr="$threshold" 'BEGIN { print (act >= thr) ? 1 : 0 }')

    if [ "$passed" -eq 1 ]; then
        status="OK"
        printf "%-25s %9.1f%%   %9.1f%%   %8s\n" "$pkg" "$actual" "$threshold" "$status"
    else
        status="FAIL"
        printf "%-25s %9.1f%%   %9.1f%%   %8s\n" "$pkg" "$actual" "$threshold" "$status"
        echo "::error file=$pkg::Coverage for $pkg is $actual%, below required threshold $threshold%"
        FAILED=1
    fi
done

echo "-------------------------------------------------------------"

# Расчет совокупного покрытия (TOTAL), исключая cmd/xcp и protobuf gen/
total_stats=$(awk '
NR>1 {
    line = $1
    # Исключить cmd/xcp и protobuf gen/
    if (line ~ /\/gen\// || line ~ /\/cmd\/xcp/ || line ~ /\/testutil/) {
        next
    }
    sub(/:.*$/, "", line)
    total += $(NF-1)
    if ($NF > 0) covered += $(NF-1)
}
END {
    if (total > 0) {
        printf "%.1f %d %d", (covered / total) * 100, covered, total
    } else {
        printf "0.0 0 0"
    }
}' "$COV_FILE")

total_actual=$(echo "$total_stats" | awk '{print $1}')
total_covered=$(echo "$total_stats" | awk '{print $2}')
total_count=$(echo "$total_stats" | awk '{print $3}')

total_passed=$(awk -v act="$total_actual" -v thr="$TOTAL_THRESHOLD" 'BEGIN { print (act >= thr) ? 1 : 0 }')

if [ "$total_passed" -eq 1 ]; then
    total_status="OK"
    printf "%-25s %9.1f%%   %9.1f%%   %8s\n" "TOTAL (internal/*)" "$total_actual" "$TOTAL_THRESHOLD" "$total_status"
else
    total_status="FAIL"
    printf "%-25s %9.1f%%   %9.1f%%   %8s\n" "TOTAL (internal/*)" "$total_actual" "$TOTAL_THRESHOLD" "$total_status"
    echo "::error::Total backend coverage is $total_actual%, below required threshold $TOTAL_THRESHOLD%"
    FAILED=1
fi
echo ""

if [ "$FAILED" -ne 0 ]; then
    echo "❌ One or more backend coverage thresholds failed!" >&2
    exit 1
fi

echo "✓ All backend coverage thresholds passed!"
exit 0
