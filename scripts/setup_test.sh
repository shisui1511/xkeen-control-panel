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
        *.gz)     [ -n "\$MOCK_GZ" ] || exit 22
                  echo "gz" >> "$TMP/gz_hits"
                  printf '#!/bin/sh\necho "$ver"\n' | gzip -c > "\$DEST" ;;
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

mock_uname_mips() {
    printf '#!/bin/sh\necho "mips"\n' > "$MOCK_BIN/uname"
    chmod +x "$MOCK_BIN/uname"
}

# Пишет файл с ELF-заголовком: $1 = EI_DATA (1 — little-endian, 2 — big-endian)
make_elf_probe() {
    ELF_PROBE_FILE="$TMP/elf_probe"
    printf "\177ELF\001\00$1\001" > "$ELF_PROBE_FILE"
}

mock_opkg_arch() {
    printf '#!/bin/sh\necho "arch all 1"\necho "arch %s 10"\n' "$1" > "$MOCK_BIN/opkg"
    chmod +x "$MOCK_BIN/opkg"
}

# curl, который первые $1 вызовов падает (панель ещё стартует), потом отвечает
mock_curl_fails_then_ok() {
    cat > "$MOCK_BIN/curl" <<EOF
#!/bin/sh
STATE_FILE="$TMP/curl_calls"
count=\$(cat "\$STATE_FILE" 2>/dev/null || echo 0)
echo \$((count+1)) > "\$STATE_FILE"
[ "\$count" -ge "$1" ]
EOF
    chmod +x "$MOCK_BIN/curl"
    printf '#!/bin/sh\nexit 1\n' > "$MOCK_BIN/wget"
    chmod +x "$MOCK_BIN/wget"
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
    XCP_ELF_PROBE="${ELF_PROBE_FILE:-/nonexistent}" \
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

# uname на 32-битных MIPS всегда "mips" — порядок байт берётся из ELF
make_sandbox
mock_opkg_not_found
mock_uname_mips
make_elf_probe 1
result=$(run_in_sandbox "detect_arch; echo \$ARCH_LABEL")
if [ "$result" = "mipsle" ]; then
    pass "uname mips + little-endian ELF → mipsle"
else
    fail "uname mips + little-endian ELF → mipsle (got: $result)"
fi
cleanup

make_sandbox
mock_opkg_not_found
mock_uname_mips
make_elf_probe 2
result=$(run_in_sandbox "detect_arch; echo \$ARCH_LABEL")
if [ "$result" = "mips" ]; then
    pass "uname mips + big-endian ELF → mips"
else
    fail "uname mips + big-endian ELF → mips (got: $result)"
fi
cleanup

make_sandbox
mock_opkg_not_found
mock_uname_mips
ELF_PROBE_FILE=""
if run_in_sandbox "detect_arch" >/dev/null 2>&1; then
    fail "uname mips без ELF-пробы должен завершаться ошибкой"
else
    pass "uname mips без ELF-пробы → ошибка, а не угаданная сборка"
fi
cleanup

# opkg приоритетнее uname
for pair in "mipsel-3.4:mipsle" "mips-3.4:mips"; do
    make_sandbox
    mock_opkg_arch "${pair%%:*}"
    mock_uname_mips
    result=$(run_in_sandbox "detect_arch; echo \$ARCH_LABEL")
    if [ "$result" = "${pair#*:}" ]; then
        pass "opkg ${pair%%:*} → ${pair#*:}"
    else
        fail "opkg ${pair%%:*} → ${pair#*:} (got: $result)"
    fi
    cleanup
done

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
if [ ! -f "$TMP/gz_hits" ]; then
    pass "Без .gz в релизе скачан несжатый бинарник"
else
    fail "Mock без .gz, но .gz был скачан"
fi
cleanup

# ---------------------------------------------------------------------------
# Test 3b: install_binary — сжатый .gz скачивается первым и распаковывается
# ---------------------------------------------------------------------------
echo ""
echo "── Обновление из .gz ─────────────────────────────────────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
install_mock_binary "v1.1.0"
mock_curl_version "v1.2.0"
mock_sha256sum_pass
mock_pgrep_not_running

export MOCK_GZ=1
rc=0
run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; install_binary" || rc=$?
unset MOCK_GZ
NEW_VER=$(run_in_sandbox "ARCH_LABEL=arm64; get_version" 2>/dev/null || echo "")
if [ "$rc" = "0" ] && [ "$NEW_VER" = "v1.2.0" ] && [ -f "$TMP/gz_hits" ]; then
    pass "Бинарник обновлён из .gz до v1.2.0"
else
    fail "Обновление из .gz: rc=$rc, версия=$NEW_VER, gz_hits=$(cat "$TMP/gz_hits" 2>/dev/null)"
fi
if ! ls /tmp/xcp.new.gz >/dev/null 2>&1; then
    pass "Временный .gz удалён"
else
    fail "Временный .gz остался в /tmp"
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
# poll_api — медленный старт не считается отказом
# ---------------------------------------------------------------------------
echo ""
echo "── poll_api ─────────────────────────────────────────────────"
make_sandbox
# 7 вызовов curl (http+https за попытку) падают — старое окно в 3 попытки
# признало бы такую панель мёртвой
mock_curl_fails_then_ok 7
if run_in_sandbox "XCP_POLL_TIMEOUT=20 XCP_POLL_INTERVAL=0 poll_api 8090" >/dev/null 2>&1; then
    pass "poll_api дожидается медленно стартующей панели"
else
    fail "poll_api сдался раньше таймаута"
fi
cleanup

make_sandbox
mock_curl_fails_then_ok 1000
if run_in_sandbox "XCP_POLL_TIMEOUT=0 XCP_POLL_INTERVAL=0 poll_api 8090" >/dev/null 2>&1; then
    fail "poll_api должен вернуть ошибку, если API так и не ответил"
else
    pass "poll_api возвращает ошибку по таймауту"
fi
cleanup

# ---------------------------------------------------------------------------
# do_migration не останавливает текущую панель
# ---------------------------------------------------------------------------
echo ""
echo "── do_migration ─────────────────────────────────────────────"
make_sandbox
mock_killall
run_in_sandbox "do_migration" >/dev/null 2>&1 || true
if grep -q "killall -q xcp" "$KILL_LOG"; then
    fail "do_migration убил текущий xcp"
else
    pass "do_migration не трогает текущий xcp"
fi
cleanup

# ---------------------------------------------------------------------------
# Откат do_update не подменяет init-скрипт давним .bak
# ---------------------------------------------------------------------------
echo ""
echo "── do_update rollback ───────────────────────────────────────"
make_sandbox
mock_opkg_not_found
mock_uname_aarch64
install_mock_binary "v1.1.0"
mock_curl_version "v1.2.0"
# API панели недоступен — do_update уйдёт в откат
sed -i '2i case "$*" in *127.0.0.1*) exit 1 ;; esac' "$MOCK_BIN/curl"
printf '#!/bin/sh\nexit 1\n' > "$MOCK_BIN/wget"; chmod +x "$MOCK_BIN/wget"
mock_sha256sum_pass
mock_killall
mock_pgrep_not_running
mock_init_script
echo "current-init" >> "$INIT_SCRIPT"
printf '#!/bin/sh\n# stale init from old install\n' > "${INIT_SCRIPT}.bak"
printf '{"port":8090}\n' > "$INSTALL_DIR/config.json"
run_in_sandbox "ARCH_LABEL=arm64; CHANNEL=stable; XCP_POLL_TIMEOUT=0; XCP_POLL_INTERVAL=0; do_update" > "$TMP/update.log" 2>&1 || true; [ -n "$DEBUG_SETUP_TEST" ] && sed "s/\x1b\[[0-9;]*m//g" "$TMP/update.log"
if grep -q "current-init" "$INIT_SCRIPT"; then
    pass "откат do_update оставил текущий init-скрипт"
else
    fail "откат do_update подменил init-скрипт давним .bak"
fi
cleanup

# ---------------------------------------------------------------------------
# pick_prerelease — канал prerelease: последний RC, если нет более нового stable
# ---------------------------------------------------------------------------
echo ""
echo "── pick_prerelease ──────────────────────────────────────────"
make_sandbox
for case in "v0.29.0-rc.9 v0.28.0 v0.29.0-rc.9" "v0.29.0-rc.9 v0.29.0 v0.29.0" \
            "v0.29.0-rc.2 v0.30.1 v0.30.1" " v0.28.0 v0.28.0" "v0.10.0-rc.1 v0.9.5 v0.10.0-rc.1"; do
    set -- $case
    if [ $# -eq 2 ]; then rc=""; stable="$1"; want="$2"; else rc="$1"; stable="$2"; want="$3"; fi
    got=$(run_in_sandbox "pick_prerelease '$rc' '$stable'")
    if [ "$got" = "$want" ]; then
        pass "pick_prerelease '${rc}' '${stable}' → ${want}"
    else
        fail "pick_prerelease '${rc}' '${stable}' → ${want} (got: $got)"
    fi
done
cleanup

# ---------------------------------------------------------------------------
# get_asset_digest / verify_checksum — эталон SHA-256 из API GitHub
# ---------------------------------------------------------------------------
echo ""
echo "── verify_checksum (digest из API) ──────────────────────────"
make_sandbox
cat > "$MOCK_BIN/curl" <<'EOF2'
#!/bin/sh
cat <<'JSON'
{"tag_name":"v1.0.0","assets":[
 {"name":"xcp_v1.0.0_mips","uploader":{"login":"x"},"digest":"sha256:1111"},
 {"name":"xcp_v1.0.0_arm64","uploader":{"login":"x"},"size":5,"digest":"sha256:2222"}]}
JSON
EOF2
chmod +x "$MOCK_BIN/curl"
got=$(run_in_sandbox "get_asset_digest v1.0.0 xcp_v1.0.0_arm64")
if [ "$got" = "2222" ]; then
    pass "get_asset_digest берёт digest своего ассета"
else
    fail "get_asset_digest берёт digest своего ассета (got: $got)"
fi
printf 'payload' > "$TMP/bin.new"
if run_in_sandbox "ARCH_LABEL=arm64; verify_checksum '$TMP/bin.new' v1.0.0" >/dev/null 2>&1; then
    fail "verify_checksum отвергает бинарник, не совпавший с digest из API"
else
    pass "verify_checksum отвергает бинарник, не совпавший с digest из API"
fi
cleanup

# Без API эталон не берётся с прокси: иначе прокси подменил бы и бинарник, и хеш
make_sandbox
cat > "$MOCK_BIN/curl" <<EOF2
#!/bin/sh
DEST=""; URL=""
for arg; do
    [ "\$prev" = "-o" ] && DEST="\$arg"
    case "\$arg" in http*) URL="\$arg" ;; esac
    prev="\$arg"
done
case "\$URL" in https://github.com/*|https://api.github.com/*) exit 7 ;; esac
[ -n "\$DEST" ] && sha256sum "$TMP/bin.new" | awk '{print \$1}' > "\$DEST"
EOF2
chmod +x "$MOCK_BIN/curl"
printf '#!/bin/sh\nexit 1\n' > "$MOCK_BIN/wget"; chmod +x "$MOCK_BIN/wget"
printf 'payload' > "$TMP/bin.new"
if run_in_sandbox "ARCH_LABEL=arm64; verify_checksum '$TMP/bin.new' v1.0.0" >/dev/null 2>&1; then
    fail "verify_checksum не доверяет хешу с прокси"
else
    pass "verify_checksum не доверяет хешу с прокси"
fi
cleanup

# ---------------------------------------------------------------------------
# do_update — GitHub недоступен: бинарник через прокси GitHub
# ---------------------------------------------------------------------------
echo ""
echo "── загрузка через прокси GitHub ─────────────────────────────"
make_sandbox
cat > "$MOCK_BIN/curl" <<EOF2
#!/bin/sh
DEST=""; URL=""
for arg; do
    [ "\$prev" = "-o" ] && DEST="\$arg"
    case "\$arg" in http*) URL="\$arg" ;; esac
    prev="\$arg"
done
echo "\$URL" >> "$TMP/urls"
[ -n "\$DEST" ] || exit 22
case "\$URL" in https://github.com/*) exit 7 ;; esac
case "\$DEST" in *.gz) exit 22 ;; esac
printf 'bin' > "\$DEST"
EOF2
chmod +x "$MOCK_BIN/curl"
printf '#!/bin/sh\nexit 1\n' > "$MOCK_BIN/wget"; chmod +x "$MOCK_BIN/wget"
src=$(run_in_sandbox "TEMP_BIN='$TMP/xcp.new'; download_from_sources https://github.com/r/xcp >/dev/null 2>&1 && echo \"\$DOWNLOAD_SOURCE\"")
if [ "$src" = "https://gh-proxy.com/" ] && [ -s "$TMP/xcp.new" ]; then
    pass "при недоступном GitHub бинарник берётся через gh-proxy.com"
else
    fail "при недоступном GitHub бинарник берётся через gh-proxy.com ($(cat "$TMP/urls" 2>/dev/null | tr '\n' ' '))"
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
