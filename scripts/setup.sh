#!/bin/sh

REPO="shisui1511/xkeen-control-panel"
BINARY="xcp"
INSTALL_DIR="${XCP_INSTALL_DIR:-/opt/etc/xcp}"
BIN_PATH="${XCP_BIN_PATH:-/opt/sbin/xcp}"
INIT_SCRIPT="${XCP_INIT_SCRIPT:-/opt/etc/init.d/S99xcp}"
DEFAULT_PORT=8090

# Цвета
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
CYAN='\033[36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { printf "${CYAN}ℹ  %s${NC}\n" "$1"; }
ok()    { printf "${GREEN}✅ %s${NC}\n" "$1"; }
warn()  { printf "${YELLOW}⚠  %s${NC}\n" "$1"; }
error() { printf "${RED}❌ %s${NC}\n" "$1"; }

log_install() {
  mkdir -p "$INSTALL_DIR"
  echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" >> "$INSTALL_DIR/install.log"
}

# Проверка языка системы
detect_lang() {
  local sys_lang
  sys_lang="ru"
  if [ -n "$LANG" ]; then
    case "$LANG" in
      en_*|en) sys_lang="en" ;;
    esac
  fi
  if [ -n "$LC_ALL" ]; then
    case "$LC_ALL" in
      en_*|en) sys_lang="en" ;;
    esac
  fi
  echo "$sys_lang"
}

# Проверка окружения (Entware)
check_entware() {
  if [ ! -d "/opt" ]; then
    error "Директория /opt не найдена! Установка невозможна."
    error "Пожалуйста, сначала установите Entware на ваш Keenetic роутер."
    error "Инструкция: https://help.keenetic.com/hc/ru/articles/360021214159"
    exit 1
  fi
  if [ ! -w "/opt" ]; then
    error "Директория /opt не имеет прав на запись!"
    error "Запустите скрипт от имени администратора (root)."
    exit 1
  fi
}

# Проверка свободного места
check_disk_space() {
  if ! command -v df >/dev/null 2>&1; then
    warn "Утилита df не найдена, пропускаем проверку свободного места."
    return 0
  fi
  
  local avail_kb
  avail_kb=$(df -k /opt 2>/dev/null | tail -n 1 | awk '{
    if ($4 ~ /^[0-9]+$/) print $4;
    else if ($3 ~ /^[0-9]+$/) print $3;
    else print "0"
  }')
  if [ -z "$avail_kb" ] || [ "$avail_kb" -eq 0 ]; then
    avail_kb=$(df -k /opt 2>/dev/null | awk 'NR>1 {print $4}' | tr -d '[:space:]')
    if [ -z "$avail_kb" ] || ! echo "$avail_kb" | grep -qE '^[0-9]+$'; then
      avail_kb=$(df -k /opt 2>/dev/null | awk 'NR>1 {print $3}' | tr -d '[:space:]')
    fi
  fi
  
  if [ -n "$avail_kb" ] && echo "$avail_kb" | grep -qE '^[0-9]+$'; then
    # 15 MB = 15360 KB
    if [ "$avail_kb" -lt 15360 ]; then
      local avail_mb
      avail_mb=$((avail_kb / 1024))
      error "Недостаточно свободного места в /opt!"
      error "Доступно: ${avail_mb} MB, требуется минимум 15 MB."
      exit 1
    fi
  else
    warn "Не удалось точно определить свободное место на диске, продолжаем."
  fi
}

# Проверка занятости порта
is_port_busy() {
  local port
  port="$1"
  if command -v netstat >/dev/null 2>&1; then
    netstat -an | grep -E "(^|[^0-9])(${port})([^0-9]|$)" | grep -iE 'listen|establish' >/dev/null 2>&1
    return $?
  elif command -v ss >/dev/null 2>&1; then
    ss -ant | grep -E "(^|[^0-9])(${port})([^0-9]|$)" | grep -iE 'listen|establish' >/dev/null 2>&1
    return $?
  fi
  return 1
}

# Опрос для ввода порта
ask_port() {
  local default_p
  local chosen_p
  local input_p
  default_p="$1"
  chosen_p="$default_p"
  
  if is_port_busy "$default_p"; then
    warn "Порт $default_p уже занят другой службой!"
    printf "Введите альтернативный порт [8090]: "
    read input_p < /dev/tty
    if [ -n "$input_p" ] && echo "$input_p" | grep -qE '^[0-9]+$'; then
      chosen_p="$input_p"
    fi
    while is_port_busy "$chosen_p"; do
      warn "Порт $chosen_p также занят!"
      printf "Введите другой порт: "
      read input_p < /dev/tty
      if [ -n "$input_p" ] && echo "$input_p" | grep -qE '^[0-9]+$'; then
        chosen_p="$input_p"
      fi
    done
  fi
  echo "$chosen_p"
}

# Определение архитектуры
detect_arch() {
  local arch
  local opkg_arch
  arch=$(uname -m)

  # Проверка через opkg (наиболее надёжно для Entware)
  if command -v opkg >/dev/null 2>&1; then
    opkg_arch=$(opkg print-architecture 2>/dev/null | grep -o 'mipsel[^[:space:]]*' | head -1)
    if [ -n "$opkg_arch" ]; then
      ARCH_LABEL="mipsle"
      return
    fi
    opkg_arch=$(opkg print-architecture 2>/dev/null | grep -o 'mips[^[:space:]]*' | head -1)
    if [ -n "$opkg_arch" ]; then
      ARCH_LABEL="mips"
      return
    fi
  fi

  # Fallback на uname
  case "$arch" in
    mipsel|mipsle)
      ARCH_LABEL="mipsle"
      ;;
    mips)
      ARCH_LABEL="mips"
      ;;
    aarch64|arm64)
      ARCH_LABEL="arm64"
      ;;
    *)
      error "Неподдерживаемая архитектура: $arch"
      exit 1
      ;;
  esac
}

# Получить latest stable версию
get_latest_stable_version() {
  local json
  local tag
  json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases/latest" || echo "")
  if [ -n "$json" ]; then
    tag=$(echo "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | head -1)
    echo "$tag"
  else
    echo ""
  fi
}

# Получить latest pre-release версию
get_latest_prerelease_version() {
  local json
  local tag
  json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases?per_page=30" || echo "")
  if [ -n "$json" ]; then
    tag=$(echo "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | grep '\-dev$' | sort -V | tail -1)
    echo "$tag"
  else
    echo ""
  fi
}

# Получить версию в зависимости от канала
get_release_version() {
  if [ "$CHANNEL" = "prerelease" ]; then
    get_latest_prerelease_version
  else
    get_latest_stable_version
  fi
}

get_github_releases_url() {
  local ver
  ver="$1"
  echo "https://github.com/${REPO}/releases/download/${ver}/xcp_${ver}_${ARCH_LABEL}"
}

get_jsdelivr_url() {
  local ver
  ver="$1"
  echo "https://cdn.jsdelivr.net/gh/${REPO}@binaries/bin/xcp_${ver}_${ARCH_LABEL}"
}

get_raw_url() {
  local ver
  ver="$1"
  echo "https://raw.githubusercontent.com/${REPO}/binaries/bin/xcp_${ver}_${ARCH_LABEL}"
}

# Трехфазная остановка сервиса
stop_service() {
  info "Останавливаем сервис..."
  if [ -f "$INIT_SCRIPT" ]; then
    "$INIT_SCRIPT" stop 2>/dev/null || true
  fi

  local count
  count=0
  while [ $count -lt 5 ]; do
    if ! pgrep -x "$BINARY" >/dev/null 2>&1; then
      break
    fi
    sleep 1
    count=$((count + 1))
  done

  if pgrep -x "$BINARY" >/dev/null 2>&1; then
    warn "Отправляем SIGTERM..."
    killall -TERM "$BINARY" 2>/dev/null || true
    sleep 2
  fi

  if pgrep -x "$BINARY" >/dev/null 2>&1; then
    warn "Отправляем SIGKILL..."
    killall -KILL "$BINARY" 2>/dev/null || true
    sleep 1
  fi
}

# Запуск сервиса
start_service() {
  if [ -f "$INIT_SCRIPT" ]; then
    info "Запускаем сервис..."
    "$INIT_SCRIPT" start 2>/dev/null || true
    sleep 1
  fi
}

# Создание конфига
create_config() {
  local port
  local sys_lang
  local config_file
  port="$1"
  config_file="$INSTALL_DIR/config.json"
  sys_lang=$(detect_lang)
  
  if [ ! -f "$config_file" ]; then
    cat > "$config_file" <<EOF
{
  "port": $port,
  "xray_config_dir": "/opt/etc/xray/configs",
  "xkeen_binary": "xkeen",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "mihomo_binary": "mihomo",
  "data_dir": "$INSTALL_DIR",
  "lang": "$sys_lang",
  "https": {
    "enabled": true,
    "cert_path": "",
    "key_path": ""
  }
}
EOF
    ok "Конфиг создан: $config_file"
    log_install "Created default config.json with port $port"
  fi
}

# Создание init-скрипта с резервным копированием существующего
create_init_script() {
  if [ -f "$INIT_SCRIPT" ]; then
    cp "$INIT_SCRIPT" "${INIT_SCRIPT}.bak"
    ok "Бэкап старого init-скрипта создан: ${INIT_SCRIPT}.bak"
    log_install "Backed up existing init script to ${INIT_SCRIPT}.bak"
  fi

  cat > "$INIT_SCRIPT" <<EOF
#!/bin/sh
ENABLED=yes
PROCS=$BINARY
ARGS="-config $INSTALL_DIR/config.json"
PREARGS=""
DESC="XKeen Control Panel"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

. /opt/etc/init.d/rc.func
EOF
  chmod +x "$INIT_SCRIPT"
  ok "Сервис создан"
  log_install "Created init script at $INIT_SCRIPT"
}

# Попытка скачать с одного URL
try_download() {
  local url
  url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsL --connect-timeout 10 --max-time 120 -o "$TEMP_BIN" "$url" 2>/dev/null
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TEMP_BIN" "$url" 2>/dev/null
  else
    return 1
  fi
}

# Проверка контрольной суммы SHA-256
verify_checksum() {
  local bin_file
  local ver
  local hash_file
  local hash_downloaded
  local expected_hash
  local actual_hash
  
  bin_file="$1"
  ver="$2"
  
  info "Проверяем SHA-256 контрольную сумму..."
  hash_file="/tmp/${BINARY}_${ver}_${ARCH_LABEL}.sha256"
  rm -f "$hash_file"
  
  hash_downloaded=false
  for source in "github" "jsdelivr" "raw"; do
    local url=""
    case "$source" in
      github) url="https://github.com/${REPO}/releases/download/${ver}/xcp_${ver}_${ARCH_LABEL}.sha256" ;;
      jsdelivr) url="https://cdn.jsdelivr.net/gh/${REPO}@binaries/bin/xcp_${ver}_${ARCH_LABEL}.sha256" ;;
      raw) url="https://raw.githubusercontent.com/${REPO}/binaries/bin/xcp_${ver}_${ARCH_LABEL}.sha256" ;;
    esac
    
    if command -v curl >/dev/null 2>&1; then
      curl -fsL --connect-timeout 10 -o "$hash_file" "$url" 2>/dev/null && hash_downloaded=true && break
    elif command -v wget >/dev/null 2>&1; then
      wget -qO "$hash_file" "$url" 2>/dev/null && hash_downloaded=true && break
    fi
  done
  
  if [ "$hash_downloaded" != "true" ]; then
    error "Сеть недоступна: не удалось загрузить файл контрольной суммы .sha256!"
    log_install "Error: sha256 checksum file unreachable"
    return 1
  fi
  
  expected_hash=$(awk '{print $1}' "$hash_file" | tr -d '[:space:]')
  if [ -z "$expected_hash" ]; then
    error "Не удалось считать хэш из файла .sha256"
    log_install "Error: expected hash is empty"
    rm -f "$hash_file"
    return 1
  fi
  
  if ! command -v sha256sum >/dev/null 2>&1; then
    warn "Утилита sha256sum не найдена, пропускаем проверку хеша"
    rm -f "$hash_file"
    return 0
  fi
  
  actual_hash=$(sha256sum "$bin_file" | awk '{print $1}')
  if [ "$expected_hash" != "$actual_hash" ]; then
    error "Критическая ошибка: SHA-256 не совпадает! Файл поврежден или изменен."
    error "Ожидалось: $expected_hash"
    error "Получено:  $actual_hash"
    log_install "Error: SHA-256 mismatch for xcp_${ver}_${ARCH_LABEL}"
    rm -f "$hash_file"
    return 1
  fi
  
  ok "SHA-256 контрольная сумма совпадает"
  rm -f "$hash_file"
  return 0
}

# Миграция со старых версий
do_migration() {
  local old_dir
  local old_bin_1
  local old_bin_2
  local old_init
  
  old_dir="/opt/etc/xkeen-control-panel"
  old_bin_1="/opt/bin/xkeen-control-panel"
  old_bin_2="/opt/sbin/xkeen-control-panel"
  old_init="/opt/etc/init.d/S99xkeen-control-panel"
  
  if [ -f "$old_init" ]; then
    "$old_init" stop 2>/dev/null || true
    rm -f "$old_init"
  fi
  killall -q "xkeen-control-panel" 2>/dev/null || true
  killall -q "xcp" 2>/dev/null || true
  
  if [ -d "$old_dir" ]; then
    info "Обнаружена старая установка. Начинаем миграцию в $INSTALL_DIR..."
    log_install "Start migration process"
    
    mkdir -p "$INSTALL_DIR"
    if [ -f "$old_dir/config.json" ]; then
      sed -e 's|/opt/etc/xkeen-control-panel|/opt/etc/xcp|g' \
          -e 's|/opt/bin/xkeen-control-panel|/opt/sbin/xcp|g' \
          "$old_dir/config.json" > "$INSTALL_DIR/config.json"
      ok "Конфигурация config.json перенесена и обновлена"
      log_install "Migrated config.json with updated paths"
    fi
    
    if [ -d "$old_dir/backup" ]; then
      cp -r "$old_dir/backup" "$INSTALL_DIR/"
      ok "Бэкапы перенесены"
      log_install "Migrated backup directory"
    fi

    if [ -d "$old_dir/ssl" ]; then
      cp -r "$old_dir/ssl" "$INSTALL_DIR/"
      ok "SSL сертификаты перенесены"
      log_install "Migrated ssl directory"
    fi
    
    rm -rf "$old_dir"
    log_install "Cleaned up old config directory"
  fi
  
  rm -f "$old_bin_1" "$old_bin_2"
}

# Определение протокола панели (http или https) на основе конфига
get_proto() {
  local proto="http"
  if [ -f "$INSTALL_DIR/config.json" ]; then
    if sed -n '/"https":/,/}/p' "$INSTALL_DIR/config.json" 2>/dev/null | grep -q '"enabled":[[:space:]]*true'; then
      proto="https"
    fi
  fi
  echo "$proto"
}

# Опрос API для проверки доступности
poll_api() {
  local port
  local url
  local https_url
  local count
  local max_tries
  local proto
  
  port="$1"
  proto=$(get_proto)
  url="http://127.0.0.1:${port}/api/auth/me"
  https_url="https://127.0.0.1:${port}/api/auth/me"
  count=1
  max_tries=3
  
  info "Проверяем доступность API по адресу ${proto}://127.0.0.1:${port}/api/auth/me..."
  
  while [ $count -le $max_tries ]; do
    if curl -k -fsL "$url" >/dev/null 2>&1 || wget --no-check-certificate -qO- "$url" >/dev/null 2>&1; then
      ok "API успешно отвечает"
      log_install "API polling succeeded on try $count"
      return 0
    fi
    if curl -k -fsL "$https_url" >/dev/null 2>&1 || wget --no-check-certificate -qO- "$https_url" >/dev/null 2>&1; then
      ok "API успешно отвечает (HTTPS)"
      log_install "API polling succeeded on try $count (HTTPS)"
      return 0
    fi
    
    warn "Попытка $count из $max_tries: API недоступен, ждем..."
    sleep 3
    count=$((count + 1))
  done
  
  return 1
}

# Загрузка бинарника
install_binary() {
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$(dirname "$BIN_PATH")"
  TEMP_BIN="/tmp/${BINARY}.new"

  info "Определяем версию..."
  LATEST_VER=$(get_release_version)
  if [ -z "$LATEST_VER" ]; then
    error "Не удалось определить версию"
    log_install "Error: failed to detect version"
    return 1
  fi
  info "Версия: $LATEST_VER"
  
  local cur_ver
  cur_ver=$(get_version)
  if [ "$cur_ver" = "$LATEST_VER" ]; then
    ok "Уже установлена последняя версия: $LATEST_VER"
    log_install "Already up to date: version $LATEST_VER"
    return 2
  fi

  # 1. Основной источник — GitHub Releases
  info "Пробуем GitHub Releases..."
  local url
  url=$(get_github_releases_url "$LATEST_VER")
  if try_download "$url"; then
    ok "Загружено с GitHub Releases"
    verify_checksum "$TEMP_BIN" "$LATEST_VER" || { rm -f "$TEMP_BIN"; return 1; }
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    log_install "Installed version $LATEST_VER from GitHub Releases"
    return 0
  fi

  # 2. Fallback — jsDelivr CDN
  warn "GitHub недоступен, пробуем jsDelivr..."
  url=$(get_jsdelivr_url "$LATEST_VER")
  if try_download "$url"; then
    ok "Загружено через jsDelivr"
    verify_checksum "$TEMP_BIN" "$LATEST_VER" || { rm -f "$TEMP_BIN"; return 1; }
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    log_install "Installed version $LATEST_VER from jsDelivr"
    return 0
  fi

  # 3. Fallback — raw.githubusercontent.com
  warn "jsDelivr недоступен, пробуем raw.githubusercontent.com..."
  url=$(get_raw_url "$LATEST_VER")
  if try_download "$url"; then
    ok "Загружено с raw.githubusercontent.com"
    verify_checksum "$TEMP_BIN" "$LATEST_VER" || { rm -f "$TEMP_BIN"; return 1; }
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    log_install "Installed version $LATEST_VER from Raw GitHub"
    return 0
  fi

  error "Все источники недоступны. Проверьте интернет"
  log_install "Error: all download sources failed"
  return 1
}

# Получение текущей версии
get_version() {
  if [ -f "$BIN_PATH" ]; then
    "$BIN_PATH" -version 2>/dev/null || echo "неизвестна"
  else
    echo "не установлена"
  fi
}

# Баннер
print_banner() {
  printf "${GREEN}${BOLD}"
  cat <<'EOF'
 █████ █████ █████   ████                                   █████████  ███████████ 
▒▒███ ▒▒███ ▒▒███   ███▒                                   ███▒▒▒▒▒███▒▒███▒▒▒▒▒███
 Profiler/Manager  ███     ██████   ██████  ████████      ███     ▒▒▒  ▒███    ▒███
   ▒▒█████    ▒███████     ███▒▒███ ███▒▒███▒▒███▒▒███    ▒███          ▒██████████ 
    ███▒███   ▒███▒▒███   ▒███████ ▒███████  ▒███ ▒███    ▒███          ▒███▒▒▒▒▒▒  
   ███ ▒▒███  ▒███ ▒▒███  ▒███▒▒▒  ▒███▒▒▒   ▒███ ▒███    ▒▒███     ███ ▒███        
   █████ █████ █████ ▒▒████▒▒██████ ▒▒██████  ████ █████    ▒▒█████████  █████       
  ▒▒▒▒▒ ▒▒▒▒▒ ▒▒▒▒▒   ▒▒▒▒  ▒▒▒▒▒▒   ▒▒▒▒▒▒  ▒▒▒▒ ▒▒▒▒▒      ▒▒▒▒▒▒▒▒▒  ▒▒▒▒▒       
EOF
  printf "${NC}\n"
}

# Установка
do_install() {
  info "Установка XKeen Control Panel ($CHANNEL)..."
  log_install "Starting installation, channel: $CHANNEL"

  check_entware
  check_disk_space
  detect_arch
  do_migration

  local chosen_port
  chosen_port="$DEFAULT_PORT"
  if [ -f "$INSTALL_DIR/config.json" ]; then
    chosen_port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
    if is_port_busy "$chosen_port"; then
      if pgrep -x "$BINARY" >/dev/null 2>&1; then
        info "Порт $chosen_port занят текущим процессом панели"
      else
        warn "Предупреждение: Порт $chosen_port занят сторонним процессом!"
      fi
    fi
  else
    if [ -n "$ARG_PORT" ]; then
      chosen_port="$ARG_PORT"
    elif [ "$INTERACTIVE" = "true" ]; then
      chosen_port=$(ask_port "$DEFAULT_PORT")
    else
      if is_port_busy "$DEFAULT_PORT"; then
        warn "Порт $DEFAULT_PORT занят. Установка продолжится, но служба может не запуститься."
      fi
    fi
  fi

  create_config "$chosen_port"
  echo "$CHANNEL" > "$INSTALL_DIR/channel"
  
  install_binary
  local install_status=$?
  if [ $install_status -eq 1 ]; then
    error "Установка прервана из-за ошибки скачивания или проверки контрольной суммы"
    return 1
  fi
  
  create_init_script
  
  if [ $install_status -ne 2 ]; then
    stop_service
    start_service
  fi
  
  poll_api "$chosen_port" || return 1

  local _ip
  _ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  _ip=${_ip:-"<IP-роутера>"}

  printf "\n${GREEN}${BOLD}========================================${NC}\n"
  printf "${GREEN}  Установка завершена!${NC}\n"
  printf "${GREEN}${BOLD}========================================${NC}\n"
  printf "  Версия:  %s\n" "$(get_version)"
  printf "  Веб-UI:  %s://%s:%s\n" "$(get_proto)" "$_ip" "$chosen_port"
  printf "  Конфиг:  %s/config.json\n" "$INSTALL_DIR"
  printf "\n  Управление:\n"
  printf "    %s start    — запуск\n" "$INIT_SCRIPT"
  printf "    %s stop     — остановка\n" "$INIT_SCRIPT"
  printf "    %s restart  — перезапуск\n" "$INIT_SCRIPT"
  printf "    %s status   — статус\n" "$INIT_SCRIPT"
  printf "${GREEN}${BOLD}========================================${NC}\n\n"
  log_install "Installation completed successfully"
}

# Обновление
do_update() {
  if [ ! -f "$BIN_PATH" ]; then
    error "Панель не установлена. Сначала установите."
    return 1
  fi

  local old_version
  old_version=$(get_version)
  info "Обновление с ${old_version} (${CHANNEL})..."
  log_install "Starting update from $old_version, channel: $CHANNEL"

  detect_arch
  echo "$CHANNEL" > "$INSTALL_DIR/channel"
  
  cp "$BIN_PATH" "${BIN_PATH}.bak"
  log_install "Created backup of current binary at ${BIN_PATH}.bak"

  install_binary
  local install_status=$?
  if [ $install_status -eq 1 ]; then
    error "Обновление прервано: не удалось скачать бинарник"
    mv "${BIN_PATH}.bak" "$BIN_PATH"
    return 1
  fi
  
  if [ $install_status -eq 2 ]; then
    rm -f "${BIN_PATH}.bak"
    return 0
  fi

  stop_service
  start_service

  local port
  port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  
  if poll_api "$port"; then
    local new_version
    new_version=$(get_version)
    ok "Обновлено успешно: ${old_version} → ${new_version}"
    log_install "Updated successfully: $old_version -> $new_version"
    rm -f "${BIN_PATH}.bak"
    return 0
  else
    error "Критическая ошибка: Новая версия не отвечает на запросы API!"
    error "Выполняется автоматический откат к предыдущей рабочей версии..."
    log_install "API polling failed. Executing automatic rollback to backup version"
    
    stop_service
    mv "${BIN_PATH}.bak" "$BIN_PATH"
    chmod +x "$BIN_PATH"
    
    if [ -f "${INIT_SCRIPT}.bak" ]; then
      mv "${INIT_SCRIPT}.bak" "$INIT_SCRIPT"
      chmod +x "$INIT_SCRIPT"
    fi
    
    start_service
    
    if poll_api "$port"; then
      ok "Откат выполнен успешно. Восстановлена версия: ${old_version}"
      log_install "Rollback succeeded. Restored version: $old_version"
    else
      error "Ошибка при восстановлении: даже старая версия не отвечает на API."
      log_install "Rollback finished, but API still unresponsive"
    fi
    return 1
  fi
}

# Удаление
do_uninstall() {
  if [ ! -f "$BIN_PATH" ] && [ ! -f "$INIT_SCRIPT" ]; then
    error "Панель не установлена."
    return
  fi

  printf "\n${RED}${BOLD}Будет удалена панель и все её файлы.${NC}\n"
  printf "Продолжить? [y/N]: "
  local response
  read response < /dev/tty
  case "$response" in
    [Yy]) ;;
    *) info "Отменено"; return ;;
  esac

  stop_service
  rm -f "$BIN_PATH"
  rm -f "${BIN_PATH}.bak"
  rm -f "$INIT_SCRIPT"
  rm -f "${INIT_SCRIPT}.bak"

  printf "\nУдалить директорию конфигов (%s)? [y/N]: " "$INSTALL_DIR"
  read response < /dev/tty
  case "$response" in
    [Yy]) rm -rf "$INSTALL_DIR"; ok "Конфиги удалены" ;;
    *) ok "Конфиги сохранены" ;;
  esac

  ok "Удаление завершено"
}

# Проверки состояния для TUI
check_entware_status() {
  STATUS_ENTWARE="ERR"
  if [ -d "/opt" ] && [ -w "/opt" ]; then
    STATUS_ENTWARE="OK"
  fi
}

check_disk_space_status() {
  STATUS_SPACE="ERR"
  SPACE_VAL="0"
  if command -v df >/dev/null 2>&1; then
    local avail_kb
    avail_kb=$(df -k /opt 2>/dev/null | tail -n 1 | awk '{
      if ($4 ~ /^[0-9]+$/) print $4;
      else if ($3 ~ /^[0-9]+$/) print $3;
      else print "0"
    }')
    if [ -z "$avail_kb" ] || [ "$avail_kb" -eq 0 ]; then
      avail_kb=$(df -k /opt 2>/dev/null | awk 'NR>1 {print $4}' | tr -d '[:space:]')
      if [ -z "$avail_kb" ] || ! echo "$avail_kb" | grep -qE '^[0-9]+$'; then
        avail_kb=$(df -k /opt 2>/dev/null | awk 'NR>1 {print $3}' | tr -d '[:space:]')
      fi
    fi
    if [ -n "$avail_kb" ] && echo "$avail_kb" | grep -qE '^[0-9]+$'; then
      SPACE_VAL=$((avail_kb / 1024))
      if [ "$avail_kb" -ge 15360 ]; then
        STATUS_SPACE="OK"
      fi
    fi
  fi
}

check_port_status() {
  local p
  p="$1"
  STATUS_PORT="OK"
  if is_port_busy "$p"; then
    STATUS_PORT="ERR"
  fi
}

# Экран «Мастер установки»
show_installer_menu() {
  check_entware_status
  check_disk_space_status
  check_port_status "$DEFAULT_PORT"

  print_banner
  printf "┌────────────────────────────────────────────────────────┐\n"
  printf "│             XKeen Control Panel Installer              │\n"
  printf "└────────────────────────────────────────────────────────┘\n\n"
  printf "Панель управления XKeen Control Panel не найдена в системе.\n"
  printf "Платформа роутера: ${CYAN}%s${NC}\n\n" "$ARCH_LABEL"
  
  printf "Проверка окружения:\n"
  if [ "$STATUS_ENTWARE" = "OK" ]; then
    printf "  [${GREEN}OK${NC}] Раздел /opt доступен на запись\n"
  else
    printf "  [${RED}ERR${NC}] Раздел /opt не доступен на запись!\n"
  fi
  
  if [ "$STATUS_SPACE" = "OK" ]; then
    printf "  [${GREEN}OK${NC}] Свободное место: %s MB (требуется >= 15 MB)\n" "$SPACE_VAL"
  else
    printf "  [${RED}ERR${NC}] Свободное место: %s MB (мало места, требуется >= 15 MB)\n" "$SPACE_VAL"
  fi

  if [ "$STATUS_PORT" = "OK" ]; then
    printf "  [${GREEN}OK${NC}] Порт %s свободен\n" "$DEFAULT_PORT"
  else
    printf "  [${YELLOW}WARN${NC}] Порт %s занят\n" "$DEFAULT_PORT"
  fi
  
  printf "\nВыберите вариант установки:\n"
  printf "  ${BOLD}1)${NC} Стандартная установка (канал Stable, порт %s)\n" "$DEFAULT_PORT"
  printf "  ${BOLD}2)${NC} Установка тестовой версии (канал Pre-release, порт %s)\n" "$DEFAULT_PORT"
  printf "  ${BOLD}0)${NC} Выход\n\n"
  printf "${GREEN}> ${NC}"
}

# Экран «Менеджер управления»
show_manager_menu() {
  local cur_version
  local port
  local channel_label
  local status_text
  local status_color
  local _ip
  local address
  local proto
  
  cur_version=$(get_version)
  port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  channel_label="Stable (стабильный)"
  if [ "$CHANNEL" = "prerelease" ]; then
    channel_label="Pre-release (тестовый)"
  fi
  
  status_text="остановлен"
  status_color="$RED"
  if pgrep -x "$BINARY" >/dev/null 2>&1; then
    status_text="активен"
    status_color="$GREEN"
  fi
  
  proto=$(get_proto)
  _ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  _ip=${_ip:-"192.168.1.1"}
  address="${proto}://${_ip}:${port}"

  print_banner
  printf "┌────────────────────────────────────────────────────────┐\n"
  printf "│             XKeen Control Panel Manager                │\n"
  printf "└────────────────────────────────────────────────────────┘\n"
  printf "  Версия:   ${CYAN}%s${NC} (${status_color}%s${NC})\n" "$cur_version" "$status_text"
  printf "  Порт:     %s\n" "$port"
  printf "  Канал:    %s\n" "$channel_label"
  printf "  Адрес:    ${CYAN}%s${NC}\n" "$address"
  printf "──────────────────────────────────────────────────────────\n\n"
  
  printf "Доступные действия по управлению:\n"
  printf "  ${BOLD}1)${NC} Проверить и установить обновления\n"
  printf "  ${BOLD}2)${NC} Управление службой (Запустить / Остановить / Перезапустить)\n"
  printf "  ${BOLD}3)${NC} Переключить канал обновлений (сейчас: %s)\n" "$([ "$CHANNEL" = "stable" ] && echo "Stable" || echo "Pre-release")"
  printf "  ${BOLD}4)${NC} Переустановить панель (сбросить конфигурацию)\n"
  printf "  ${BOLD}5)${NC} Удалить панель из системы\n"
  printf "  ${BOLD}0)${NC} Выход\n\n"
  printf "${GREEN}> ${NC}"
}

# Подменю управления службой
manage_service_menu() {
  local choice
  local status_text
  local status_color
  while true; do
    print_banner
    status_text="остановлен"
    status_color="$RED"
    if pgrep -x "$BINARY" >/dev/null 2>&1; then
      status_text="активен"
      status_color="$GREEN"
    fi
    printf "Управление службой XKeen Control Panel (статус: ${status_color}%s${NC})\n\n" "$status_text"
    printf "  1) Запустить службу\n"
    printf "  2) Остановить службу\n"
    printf "  3) Перезапустить службу\n"
    printf "  0) Назад\n\n"
    printf "${GREEN}> ${NC}"
    read choice < /dev/tty || return
    
    case "$choice" in
      1) start_service; sleep 1 ;;
      2) stop_service; sleep 1 ;;
      3) stop_service; start_service; sleep 1 ;;
      0) return ;;
      *) error "Неверный выбор" ;;
    esac
  done
}

switch_channel() {
  if [ "$CHANNEL" = "stable" ]; then
    CHANNEL="prerelease"
    ok "Канал переключен на Pre-release (тестовые сборки)"
  else
    CHANNEL="stable"
    ok "Канал переключен на Stable (стабильные сборки)"
  fi
  mkdir -p "$INSTALL_DIR"
  echo "$CHANNEL" > "$INSTALL_DIR/channel"
}

status_service() {
  if pgrep -x "$BINARY" >/dev/null 2>&1; then
    local pid
    pid=$(pgrep -x "$BINARY")
    ok "Служба $BINARY запущена (PID: $pid)"
    return 0
  else
    warn "Служба $BINARY остановлена"
    return 3
  fi
}

# Обработка CLI-аргументов
parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --install|-i|install)
        INTERACTIVE="false"
        ACTION="install"
        shift
        ;;
      --update|-u|update)
        INTERACTIVE="false"
        ACTION="update"
        shift
        ;;
      --uninstall|-d|uninstall)
        INTERACTIVE="false"
        ACTION="uninstall"
        shift
        ;;
      --restart|-r|restart)
        INTERACTIVE="false"
        ACTION="restart"
        shift
        ;;
      --status|-s|status)
        INTERACTIVE="false"
        ACTION="status"
        shift
        ;;
      --channel|-c)
        if [ -n "$2" ]; then
          CHANNEL="$2"
          shift 2
        else
          error "Параметр --channel требует значение (stable/prerelease)"
          exit 1
        fi
        ;;
      --port|-p)
        if [ -n "$2" ]; then
          ARG_PORT="$2"
          shift 2
        else
          error "Параметр --port требует значение"
          exit 1
        fi
        ;;
      --help|-h|help)
        print_banner
        printf "Использование: setup.sh [ОПЦИИ]\n\n"
        printf "Опции:\n"
        printf "  -i, --install      Установка панели (неинтерактивно)\n"
        printf "  -u, --update       Обновление панели\n"
        printf "  -d, --uninstall    Удаление панели\n"
        printf "  -r, --restart      Перезапуск службы панели\n"
        printf "  -s, --status       Проверить статус службы\n"
        printf "  -c, --channel CH   Канал обновления (stable/prerelease)\n"
        printf "  -p, --port PORT    Порт для веб-интерфейса панели\n"
        printf "  -h, --help         Показать эту справку\n\n"
        exit 0
        ;;
      *)
        error "Неизвестный параметр: $1"
        exit 1
        ;;
    esac
  done
}

# ===== Главный цикл =====

# Allow test harness to source functions without executing main
[ -n "$SETUP_TEST_MODE" ] && return 0 2>/dev/null; true

INTERACTIVE="true"
CHANNEL="stable"
ARG_PORT=""

# Считываем сохраненный канал обновлений, если файл существует
if [ -f "$INSTALL_DIR/channel" ]; then
  CHANNEL=$(cat "$INSTALL_DIR/channel" | tr -d '[:space:]')
fi

# Считываем аргументы
parse_args "$@"

# Автоопределение архитектуры роутера
detect_arch

# Автоопределение возможности интерактива
if [ "$INTERACTIVE" = "true" ]; then
  if [ ! -r /dev/tty ] || [ ! -t 1 ]; then
    INTERACTIVE="false"
    ACTION="install"
  fi
fi

# Неинтерактивное выполнение
if [ "$INTERACTIVE" = "false" ]; then
  case "$ACTION" in
    install)
      do_install
      exit $?
      ;;
    update)
      do_update
      exit $?
      ;;
    uninstall)
      do_uninstall
      exit $?
      ;;
    restart)
      stop_service
      start_service
      exit $?
      ;;
    status)
      status_service
      exit $?
      ;;
    *)
      # По умолчанию ставим stable
      do_install
      exit $?
      ;;
  esac
fi

# Интерактивный режим
if [ -f "$BIN_PATH" ]; then
  # Режим «Менеджер управления»
  while true; do
    show_manager_menu
    read choice < /dev/tty || {
      warn "Терминал недоступен, выход."
      exit 1
    }
    
    case "$choice" in
      1)
        do_update
        ;;
      2)
        manage_service_menu
        ;;
      3)
        switch_channel
        ;;
      4)
        printf "\nПереустановить панель и сбросить конфиг? [y/N]: "
        read response < /dev/tty
        case "$response" in
          [Yy])
            rm -f "$INSTALL_DIR/config.json"
            do_install
            ;;
          *)
            info "Отменено"
            ;;
        esac
        ;;
      5)
        do_uninstall
        if [ ! -f "$BIN_PATH" ]; then
          exit 0
        fi
        ;;
      0)
        ok "До свидания!"
        exit 0
        ;;
      *)
        error "Неверный выбор"
        ;;
    esac
    printf "\nНажмите Enter для продолжения..."
    read dummy < /dev/tty || true
  done
else
  # Режим «Мастер установки»
  while true; do
    show_installer_menu
    read choice < /dev/tty || {
      warn "Терминал недоступен, запускаем стандартную установку..."
      CHANNEL="stable"
      do_install
      exit $?
    }
    
    case "$choice" in
      1)
        CHANNEL="stable"
        do_install
        exit $?
        ;;
      2)
        CHANNEL="prerelease"
        do_install
        exit $?
        ;;
      0)
        ok "До свидания!"
        exit 0
        ;;
      *)
        error "Неверный выбор"
        ;;
    esac
    printf "\nНажмите Enter для продолжения..."
    read dummy < /dev/tty || true
  done
fi
