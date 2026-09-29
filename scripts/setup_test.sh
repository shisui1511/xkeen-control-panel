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
    XCP_TTY_DEV="${TTY_DEV_OVERRIDE:-$TMP/no-tty}" \
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
# get_proto — панель только по HTTPS (D-12), флаг конфига не действует
# ---------------------------------------------------------------------------
echo ""
echo "── get_proto ────────────────────────────────────────────────"
make_sandbox
mkdir -p "$INSTALL_DIR"
printf '{"https":{"enabled":false}}\n' > "$INSTALL_DIR/config.json"
result=$(run_in_sandbox "get_proto")
if [ "$result" = "https" ]; then
    pass "get_proto отдаёт https даже при enabled:false в конфиге"
else
    fail "get_proto отдаёт https даже при enabled:false в конфиге (got: $result)"
fi
cleanup

make_sandbox
result=$(run_in_sandbox "get_proto")
if [ "$result" = "https" ]; then
    pass "get_proto отдаёт https без config.json вообще"
else
    fail "get_proto отдаёт https без config.json вообще (got: $result)"
fi
cleanup

# ---------------------------------------------------------------------------
# offer_password_setup — предложение задать пароль или код настройки (D-25)
# ---------------------------------------------------------------------------
echo ""
echo "── offer_password_setup ─────────────────────────────────────"

# mock-бинарник xcp: --setup-code печатает фиксированный код, --reset-password
# логирует факт вызова (чтобы тесты могли утверждать, что он НЕ вызывался)
install_mock_xcp_setup_code() {
    cat > "$BIN_PATH" <<'EOF'
#!/bin/sh
case "$1" in
    --setup-code) echo "A1B2C3D4" ;;
    --reset-password) echo "reset-password called" >> "$XCP_CALL_LOG"; exit 0 ;;
esac
EOF
    chmod +x "$BIN_PATH"
}

make_sandbox
install_mock_xcp_setup_code
printf '{}\n' > "$INSTALL_DIR/config.json"
export XCP_CALL_LOG="$TMP/xcp_calls.log"
touch "$XCP_CALL_LOG"
out=$(XCP_CALL_LOG="$XCP_CALL_LOG" run_in_sandbox "INTERACTIVE=false; offer_password_setup 8090 192.168.1.1" 2>&1)
if echo "$out" | grep -q "https://192.168.1.1:8090" && echo "$out" | grep -q "A1B2C3D4"; then
    pass "offer_password_setup (неинтерактивно) печатает https-адрес и код"
else
    fail "offer_password_setup (неинтерактивно) печатает https-адрес и код (got: $out)"
fi
if [ -s "$XCP_CALL_LOG" ]; then
    fail "offer_password_setup (неинтерактивно) не должен вызывать --reset-password"
else
    pass "offer_password_setup (неинтерактивно) не вызывает --reset-password"
fi
unset XCP_CALL_LOG
cleanup

make_sandbox
install_mock_xcp_setup_code
password_hash_json='{"auth":{"password_hash":"$2a$10$abcdefghijklmnopqrstuv"}}'
printf '%s\n' "$password_hash_json" > "$INSTALL_DIR/config.json"
export XCP_CALL_LOG="$TMP/xcp_calls.log"
touch "$XCP_CALL_LOG"
out=$(XCP_CALL_LOG="$XCP_CALL_LOG" run_in_sandbox "INTERACTIVE=false; offer_password_setup 8090 192.168.1.1" 2>&1)
if [ -z "$out" ]; then
    pass "offer_password_setup молчит, если пароль уже задан"
else
    fail "offer_password_setup молчит, если пароль уже задан (got: $out)"
fi
if [ -s "$XCP_CALL_LOG" ]; then
    fail "offer_password_setup с заданным паролем не должен вызывать mock xcp"
else
    pass "offer_password_setup с заданным паролем не вызывает mock xcp"
fi
unset XCP_CALL_LOG
cleanup

# ---------------------------------------------------------------------------
# do_uninstall — удаление без терминала и с ним (D-13)
# ---------------------------------------------------------------------------
echo ""
echo "── do_uninstall ─────────────────────────────────────────────"

# Песочница с установленной панелью: бинарник, init-скрипт и данные
setup_uninstall_sandbox() {
    make_sandbox
    install_mock_binary "0.1.0"
    mock_init_script
    printf '{"auth":{"password_hash":"x"}}\n' > "$INSTALL_DIR/config.json"
    mock_pgrep_not_running
    mock_killall
}

setup_uninstall_sandbox
out=$(run_in_sandbox "do_uninstall; echo rc=\$?" </dev/null 2>&1)
if [ ! -e "$BIN_PATH" ] && [ ! -e "$INIT_SCRIPT" ] \
    && [ -f "$INSTALL_DIR/config.json" ] \
    && echo "$out" | grep -q "rc=0" \
    && ! echo "$out" | grep -q "Отменено"; then
    pass "uninstall без TTY сохраняет данные"
else
    fail "uninstall без TTY сохраняет данные (got: $out)"
fi
cleanup

# --uninstall --purge без терминала удаляет и данные панели
setup_uninstall_sandbox
out=$(run_in_sandbox "ARG_PURGE=true; do_uninstall; echo rc=\$?" </dev/null 2>&1)
if [ ! -e "$BIN_PATH" ] && [ ! -e "$INIT_SCRIPT" ] && [ ! -e "$INSTALL_DIR" ] \
    && echo "$out" | grep -q "rc=0"; then
    pass "uninstall --purge без TTY удаляет данные"
else
    fail "uninstall --purge без TTY удаляет данные (got: $out)"
fi
cleanup

# Разбор аргументов: порядок флагов не важен
setup_uninstall_sandbox
out=$(run_in_sandbox "parse_args --purge --uninstall; echo \"\$ACTION \$ARG_PURGE\"" 2>&1) || true
if [ "$out" = "uninstall true" ]; then
    pass "parse_args: --purge --uninstall → ACTION=uninstall, ARG_PURGE=true"
else
    fail "parse_args: --purge --uninstall → ACTION=uninstall, ARG_PURGE=true (got: $out)"
fi
out=$(run_in_sandbox "parse_args --uninstall --purge; echo \"\$ACTION \$ARG_PURGE\"" 2>&1) || true
if [ "$out" = "uninstall true" ]; then
    pass "parse_args: --uninstall --purge → ACTION=uninstall, ARG_PURGE=true"
else
    fail "parse_args: --uninstall --purge → ACTION=uninstall, ARG_PURGE=true (got: $out)"
fi
cleanup

# --purge без --uninstall — ошибка, ничего не удалено
setup_uninstall_sandbox
rc=0
out=$(run_in_sandbox "parse_args --purge" 2>&1) || rc=$?
if [ "$rc" -eq 1 ] && echo "$out" | grep -q "Флаг --purge работает только вместе с --uninstall" \
    && [ -e "$BIN_PATH" ] && [ -e "$INIT_SCRIPT" ] && [ -f "$INSTALL_DIR/config.json" ]; then
    pass "--purge без --uninstall — ошибка"
else
    fail "--purge без --uninstall — ошибка (rc=$rc, got: $out)"
fi
cleanup

# purge_install_dir отказывается от пустого и системных путей (rm — mock)
make_sandbox
printf '#!/bin/sh\necho "$*" >> "%s/rm.log"\n' "$TMP" > "$MOCK_BIN/rm"
chmod +x "$MOCK_BIN/rm"
guard_ok=true
for bad in "" "/" "/opt" "/opt/" "/opt/etc" "/opt/etc/"; do
    rc=0
    out=$(run_in_sandbox "INSTALL_DIR='$bad'; purge_install_dir" 2>&1) || rc=$?
    if [ "$rc" -ne 1 ]; then
        guard_ok=false
        printf "    путь '%s': код %s вместо 1\n" "$bad" "$rc"
    fi
done
if [ "$guard_ok" = "true" ] && [ ! -s "$TMP/rm.log" ]; then
    pass "purge_install_dir отказывается от системных путей"
else
    fail "purge_install_dir отказывается от системных путей (rm.log: $(cat "$TMP/rm.log" 2>/dev/null))"
fi
cleanup

# Обычный путь данных purge_install_dir удаляет
setup_uninstall_sandbox
out=$(run_in_sandbox "purge_install_dir; echo rc=\$?" 2>&1)
if [ ! -e "$INSTALL_DIR" ] && echo "$out" | grep -q "rc=0"; then
    pass "purge_install_dir удаляет обычный каталог данных"
else
    fail "purge_install_dir удаляет обычный каталог данных (got: $out)"
fi
cleanup

# Интерактив (терминал = файл с ответами): «n» отменяет, ничего не удалено
setup_uninstall_sandbox
printf 'n\n' > "$TMP/tty"
TTY_DEV_OVERRIDE="$TMP/tty"
out=$(run_in_sandbox "ARG_PURGE=true; do_uninstall" 2>&1)
unset TTY_DEV_OVERRIDE
if echo "$out" | grep -q "Продолжить? \[y/N\]" && echo "$out" | grep -q "Отменено" \
    && [ -e "$BIN_PATH" ] && [ -e "$INIT_SCRIPT" ] && [ -f "$INSTALL_DIR/config.json" ]; then
    pass "интерактив: ответ n отменяет"
else
    fail "интерактив: ответ n отменяет (got: $out)"
fi
cleanup

# Интерактив: «y» + --purge — второй вопрос не задаётся, данные удалены
setup_uninstall_sandbox
printf 'y\n' > "$TMP/tty"
TTY_DEV_OVERRIDE="$TMP/tty"
out=$(run_in_sandbox "ARG_PURGE=true; do_uninstall" 2>&1)
unset TTY_DEV_OVERRIDE
if [ ! -e "$BIN_PATH" ] && [ ! -e "$INIT_SCRIPT" ] && [ ! -e "$INSTALL_DIR" ] \
    && ! echo "$out" | grep -q "Удалить директорию конфигов"; then
    pass "интерактив: ответ y + purge"
else
    fail "интерактив: ответ y + purge (got: $out)"
fi
cleanup

# Интерактив без --purge: второй вопрос задаётся как раньше. Файл-терминал
# открывается заново при каждом чтении, поэтому разные ответы на два вопроса
# подаёт заглушка read: построчно из файла ответов.
write_read_stub() {
    cat > "$TMP/read_stub.sh" <<'STUB'
read() {
    _n=$(cat "$STUB_DIR/n" 2>/dev/null || echo 0)
    _n=$((_n+1))
    echo "$_n" > "$STUB_DIR/n"
    _ans=$(sed -n "${_n}p" "$STUB_DIR/answers")
    eval "$1=\$_ans"
}
STUB
}

setup_uninstall_sandbox
write_read_stub
printf 'y\nn\n' > "$TMP/answers"
printf 'stub\n' > "$TMP/tty"
TTY_DEV_OVERRIDE="$TMP/tty"
out=$(STUB_DIR="$TMP" run_in_sandbox ". '$TMP/read_stub.sh'; do_uninstall" 2>&1)
unset TTY_DEV_OVERRIDE
if [ ! -e "$BIN_PATH" ] && [ ! -e "$INIT_SCRIPT" ] && [ -f "$INSTALL_DIR/config.json" ] \
    && echo "$out" | grep -q "Удалить директорию конфигов"; then
    pass "интерактив: второй вопрос про конфиги, ответ n сохраняет данные"
else
    fail "интерактив: второй вопрос про конфиги, ответ n сохраняет данные (got: $out)"
fi
cleanup

setup_uninstall_sandbox
printf 'y\ny\n' > "$TMP/tty"
TTY_DEV_OVERRIDE="$TMP/tty"
out=$(run_in_sandbox "do_uninstall" 2>&1)
unset TTY_DEV_OVERRIDE
if [ ! -e "$BIN_PATH" ] && [ ! -e "$INSTALL_DIR" ]; then
    pass "интерактив: второй вопрос, ответ y удаляет данные"
else
    fail "интерактив: второй вопрос, ответ y удаляет данные (got: $out)"
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
