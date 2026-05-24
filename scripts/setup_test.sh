#!/bin/sh
# scripts/setup_test.sh — интеграционные тесты для setup.sh
# Запуск: sh scripts/setup_test.sh
# Проверяет: идемпотентность установки, остановку при обновлении, force-kill

set -e

PASS=0
FAIL=0
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SETUP="$SCRIPT_DIR/setup.sh"

pass() { PASS=$((PASS+1)); printf "  PASS  %s\n" "$1"; }
fail() { FAIL=$((FAIL+1)); printf "  FAIL  %s\n" "$1"; }

# ---------------------------------------------------------------------------
# Вспомогательные функции
# ---------------------------------------------------------------------------

# Создаёт изолированную sandbox-среду в tmp
make_sandbox() {
    TMP=$(mktemp -d)
    MOCK_BIN="$TMP/bin"
    INSTALL_DIR="$TMP/etc/xcp"
    BIN_PATH="$TMP/sbin/xcp"
    INIT_SCRIPT="$TMP/etc/init.d/S99xcp"
    KILL_LOG="$TMP/kill.log"

    mkdir -p "$MOCK_BIN" "$INSTALL_DIR" "$(dirname "$BIN_PATH")" "$(dirname "$INIT_SCRIPT")"
    touch "$KILL_LOG"
}

# Устанавливает mock-двоичник xcp с заданной версией
install_mock_binary() {
    local ver="$1"
    printf '#!/bin/sh\necho "%s"\n' "$ver" > "$BIN_PATH"
    chmod +x "$BIN_PATH"
}

# Создаёт mock curl, возвращающий json с указанной версией
mock_curl_version() {
    local ver="$1"
    cat > "$MOCK_BIN/curl" <<EOF
#!/bin/sh
# Simulate GitHub API: return version JSON or write dummy binary to -o <dest>
DEST=""
for arg; do
    if [ "\$prev" = "-o" ]; then DEST="\$arg"; fi
    prev="\$arg"
done
if [ -n "\$DEST" ]; then
    # Write a sha256 file or a dummy executable
    case "\$DEST" in
        *.sha256) sha256sum "$BIN_PATH" 2>/dev/null | awk '{print \$1}' > "\$DEST" || echo "dummy" > "\$DEST" ;;
        *)        printf '#!/bin/sh\necho "$ver"\n' > "\$DEST"; chmod +x "\$DEST" ;;
    esac
    exit 0
fi
# Respond to GitHub API version query
printf '{"tag_name":"%s"}\n' "$ver"
EOF
    chmod +x "$MOCK_BIN/curl"
}

# pgrep qui renvoie 0 (process running) pendant N appels, puis 1
mock_pgrep_running_then_stops() {
    local calls="$1"   # how many times to report running (0 = never running)
    cat > "$MOCK_BIN/pgrep" <<EOF
#!/bin/sh
STATE_FILE="$TMP/pgrep_state"
count=\$(cat "\$STATE_FILE" 2>/dev/null || echo 0)
if [ "\$count" -lt "$calls" ]; then
    echo \$((count+1)) > "\$STATE_FILE"
    echo 42
    exit 0
fi
exit 1
EOF
    chmod +x "$MOCK_BIN/pgrep"
}

mock_pgrep_not_running() {
    printf '#!/bin/sh\nexit 1\n' > "$MOCK_BIN/pgrep"
    chmod +x "$MOCK_BIN/pgrep"
}

mock_killall() {
    cat > "$MOCK_BIN/killall" <<EOF
#!/bin/sh
echo "killall \$*" >> "$KILL_LOG"
exit 0
EOF
    chmod +x "$MOCK_BIN/killall"
}

mock_opkg_not_found() {
    # opkg not on PATH → detect_arch falls back to uname
    rm -f "$MOCK_BIN/opkg"
}

mock_uname_aarch64() {
    printf '#!/bin/sh\necho "aarch64"\n' > "$MOCK_BIN/uname"
    chmod +x "$MOCK_BIN/uname"
}

mock_sha256sum_pass() {
    cat > "$MOCK_BIN/sha256sum" <<'EOF'
#!/bin/sh
# Print the hash so verify_checksum can read it; always "matches"
printf "abc123  %s\n" "$1"
EOF
    chmod +x "$MOCK_BIN/sha256sum"
}

mock_init_script() {
    printf '#!/bin/sh\nexit 0\n' > "$INIT_SCRIPT"
    chmod +x "$INIT_SCRIPT"
}

cleanup() { rm -rf "$TMP"; }

run_in_sandbox() {
    # Runs the given function from setup.sh inside the sandbox environment
    SETUP_TEST_MODE=1 \
    XCP_INSTALL_DIR="$INSTALL_DIR" \
    XCP_BIN_PATH="$BIN_PATH" \
    XCP_INIT_SCRIPT="$INIT_SCRIPT" \
    PATH="$MOCK_BIN:$PATH" \
    sh -c ". '$SETUP'; $1"
}

# ---------------------------------------------------------------------------
# Test 1: detect_arch — aarch64 → arm64
# ---------------------------------------------------------------------------
echo ""
echo "── detect_arch ──────────────────────────────────────────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
result=$(run_in_sandbox "detect_arch; echo \$ARCH_LABEL")
if [ "$result" = "arm64" ]; then
    pass "aarch64 → arm64"
else
    fail "aarch64 → arm64 (got: $result)"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 2: install_binary — идемпотентность (уже актуальная версия)
# ---------------------------------------------------------------------------
echo ""
echo "── Идемпотентность (install_binary) ─────────────────────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
install_mock_binary "v1.2.0"
mock_curl_version "v1.2.0"
mock_sha256sum_pass

# install_binary should return exit code 2 (already up to date)
rc=0
run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; install_binary" || rc=$?

if [ "$rc" = "2" ]; then
    pass "install_binary возвращает 2 (already up to date)"
else
    fail "install_binary: ожидали rc=2, получили rc=$rc"
fi

# Binary must not be replaced
INODE_BEFORE=$(stat -c %i "$BIN_PATH" 2>/dev/null || stat -f %i "$BIN_PATH" 2>/dev/null)
run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; install_binary" 2>/dev/null || true
INODE_AFTER=$(stat -c %i "$BIN_PATH" 2>/dev/null || stat -f %i "$BIN_PATH" 2>/dev/null)
if [ "$INODE_BEFORE" = "$INODE_AFTER" ]; then
    pass "Бинарник не заменён при актуальной версии"
else
    fail "Бинарник был заменён несмотря на актуальную версию"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 3: install_binary — новая версия заменяет бинарник
# ---------------------------------------------------------------------------
echo ""
echo "── Обновление бинарника ──────────────────────────────────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
install_mock_binary "v1.1.0"
mock_curl_version "v1.2.0"
mock_sha256sum_pass
mock_pgrep_not_running

rc=0
run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; install_binary" || rc=$?
if [ "$rc" = "0" ]; then
    pass "install_binary возвращает 0 при обновлении"
else
    fail "install_binary: ожидали rc=0, получили rc=$rc"
fi

NEW_VER=$(run_in_sandbox "ARCH_LABEL=arm64; get_version" 2>/dev/null || echo "")
if [ "$NEW_VER" = "v1.2.0" ]; then
    pass "Бинарник обновлён до v1.2.0"
else
    fail "Бинарник не обновлён (версия: $NEW_VER)"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 4: stop_service — вызывает killall при работающем процессе
# ---------------------------------------------------------------------------
echo ""
echo "── stop_service — SIGTERM при работающем процессе ───────────"
make_sandbox
mock_killall
mock_init_script
# pgrep reports running for first 6 calls (5 poll + 1 check), then stops
mock_pgrep_running_then_stops 6

run_in_sandbox "stop_service" 2>/dev/null || true

if grep -q "killall" "$KILL_LOG"; then
    pass "stop_service вызвал killall"
else
    fail "stop_service не вызвал killall"
fi

if grep -q "\-TERM" "$KILL_LOG"; then
    pass "stop_service отправил SIGTERM"
else
    fail "stop_service не отправил SIGTERM"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 5: stop_service — force-kill (SIGKILL) если SIGTERM не помогает
# ---------------------------------------------------------------------------
echo ""
echo "── stop_service — SIGKILL при зависшем процессе ─────────────"
make_sandbox
mock_killall
mock_init_script
# Process never stops → pgrep always returns running
mock_pgrep_running_then_stops 999

run_in_sandbox "stop_service" 2>/dev/null || true

if grep -q "\-KILL" "$KILL_LOG"; then
    pass "stop_service отправил SIGKILL при зависшем процессе"
else
    fail "stop_service не отправил SIGKILL"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 6: do_update — вызывает stop_service перед заменой бинарника
# ---------------------------------------------------------------------------
echo ""
echo "── do_update — остановка сервиса перед обновлением ──────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
install_mock_binary "v1.1.0"
mock_curl_version "v1.2.0"
mock_sha256sum_pass
mock_killall
mock_init_script
# Process reports running for initial checks, then stops after killall
mock_pgrep_running_then_stops 7

# Create a minimal config so do_update doesn't fail on port read
printf '{"port":8090}\n' > "$INSTALL_DIR/config.json"

run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; do_update" 2>/dev/null || true

if grep -q "killall" "$KILL_LOG"; then
    pass "do_update вызвал stop_service (killall найден в логе)"
else
    fail "do_update не вызвал stop_service"
fi
cleanup

# ---------------------------------------------------------------------------
# Итог
# ---------------------------------------------------------------------------
echo ""
echo "══════════════════════════════════════════════════════════════"
echo "  Всего: $((PASS+FAIL))  |  Пройдено: $PASS  |  Провалено: $FAIL"
echo "══════════════════════════════════════════════════════════════"

[ "$FAIL" -eq 0 ]
