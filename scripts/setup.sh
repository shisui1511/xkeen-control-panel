#!/bin/sh

REPO="shisui1511/xkeen-control-panel"
BINARY="xcp"
INSTALL_DIR="/opt/etc/xcp"
BIN_PATH="/opt/sbin/xcp"
INIT_SCRIPT="/opt/etc/init.d/S99xcp"
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
  SYS_LANG="ru"
  if [ -n "$LANG" ]; then
    case "$LANG" in
      en_*|en) SYS_LANG="en" ;;
    esac
  fi
  if [ -n "$LC_ALL" ]; then
    case "$LC_ALL" in
      en_*|en) SYS_LANG="en" ;;
    esac
  fi
  echo "$SYS_LANG"
}

# Определение архитектуры
detect_arch() {
  ARCH=$(uname -m)

  # Проверка через opkg (наиболее надёжно для Entware)
  if command -v opkg >/dev/null 2>&1; then
    OPKG_ARCH=$(opkg print-architecture 2>/dev/null | grep -o 'mipsel[^[:space:]]*' | head -1)
    if [ -n "$OPKG_ARCH" ]; then
      ARCH_LABEL="mipsle"
      return
    fi
    OPKG_ARCH=$(opkg print-architecture 2>/dev/null | grep -o 'mips[^[:space:]]*' | head -1)
    if [ -n "$OPKG_ARCH" ]; then
      ARCH_LABEL="mips"
      return
    fi
  fi

  # Fallback на uname
  case "$ARCH" in
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
      error "Неподдерживаемая архитектура: $ARCH"
      exit 1
      ;;
  esac
}

# Получить latest stable версию
get_latest_stable_version() {
  _json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases/latest" || echo "")
  if [ -n "$_json" ]; then
    _tag=$(echo "$_json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | head -1)
    echo "$_tag"
  else
    echo ""
  fi
}

# Получить latest pre-release версию
get_latest_prerelease_version() {
  _json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases?per_page=30" || echo "")
  if [ -n "$_json" ]; then
    _tag=$(echo "$_json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | grep '\-dev$' | sort -V | tail -1)
    echo "$_tag"
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
  _ver="$1"
  echo "https://github.com/${REPO}/releases/download/${_ver}/xcp_${_ver}_${ARCH_LABEL}"
}

get_jsdelivr_url() {
  _ver="$1"
  echo "https://cdn.jsdelivr.net/gh/${REPO}@binaries/bin/xcp_${_ver}_${ARCH_LABEL}"
}

get_raw_url() {
  _ver="$1"
  echo "https://raw.githubusercontent.com/${REPO}/binaries/bin/xcp_${_ver}_${ARCH_LABEL}"
}

# Трехфазная остановка сервиса
stop_service() {
  info "Останавливаем сервис..."
  # 1. Попытка graceful остановки через init-скрипт
  if [ -f "$INIT_SCRIPT" ]; then
    "$INIT_SCRIPT" stop 2>/dev/null || true
  fi

  # 2. Ожидание завершения процесса до 5 секунд
  local count=0
  while [ $count -lt 5 ]; do
    if ! pgrep -x "$BINARY" >/dev/null 2>&1; then
      break
    fi
    sleep 1
    count=$((count + 1))
  done

  # 3. Принудительная остановка: SIGTERM, затем SIGKILL
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
  CONFIG_FILE="$INSTALL_DIR/config.json"
  SYS_LANG=$(detect_lang)
  if [ ! -f "$CONFIG_FILE" ]; then
    cat > "$CONFIG_FILE" <<EOF
{
  "port": $DEFAULT_PORT,
  "xray_config_dir": "/opt/etc/xray/configs",
  "xkeen_binary": "xkeen",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "mihomo_binary": "mihomo",
  "data_dir": "$INSTALL_DIR",
  "lang": "$SYS_LANG",
  "https": {
    "enabled": true,
    "cert_path": "",
    "key_path": ""
  }
}
EOF
    ok "Конфиг создан: $CONFIG_FILE"
    log_install "Created default config.json"
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
  _url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsL --connect-timeout 10 --max-time 120 -o "$TEMP_BIN" "$_url" 2>/dev/null
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TEMP_BIN" "$_url" 2>/dev/null
  else
    return 1
  fi
}

# Проверка контрольной суммы SHA-256
verify_checksum() {
  local bin_file="$1"
  local ver="$2"
  
  info "Проверяем SHA-256 контрольную сумму..."
  local cs_file="/tmp/checksums.txt"
  rm -f "$cs_file"
  
  local cs_downloaded=false
  for source in "github" "jsdelivr" "raw"; do
    local url=""
    case "$source" in
      github) url="https://github.com/${REPO}/releases/download/${ver}/checksums.txt" ;;
      jsdelivr) url="https://cdn.jsdelivr.net/gh/${REPO}@binaries/bin/checksums.txt" ;;
      raw) url="https://raw.githubusercontent.com/${REPO}/binaries/bin/checksums.txt" ;;
    esac
    
    if command -v curl >/dev/null 2>&1; then
      curl -fsL --connect-timeout 10 -o "$cs_file" "$url" 2>/dev/null && cs_downloaded=true && break
    elif command -v wget >/dev/null 2>&1; then
      wget -qO "$cs_file" "$url" 2>/dev/null && cs_downloaded=true && break
    fi
  done
  
  if [ "$cs_downloaded" != "true" ]; then
    error "Сеть недоступна: не удалось загрузить checksums.txt для проверки целостности бинарника!"
    log_install "Error: checksums.txt unreachable"
    return 1
  fi
  
  local expected_hash=$(grep "xcp_${ver}_${ARCH_LABEL}" "$cs_file" | awk '{print $1}')
  if [ -z "$expected_hash" ]; then
    error "Хэш для xcp_${ver}_${ARCH_LABEL} не найден в checksums.txt"
    log_install "Error: hash not found in checksums.txt"
    return 1
  fi
  
  if ! command -v sha256sum >/dev/null 2>&1; then
    warn "Утилита sha256sum не найдена, пропускаем проверку хеша"
    return 0
  fi
  
  local actual_hash=$(sha256sum "$bin_file" | awk '{print $1}')
  if [ "$expected_hash" != "$actual_hash" ]; then
    error "Критическая ошибка: SHA-256 не совпадает! Файл поврежден или изменен."
    error "Ожидалось: $expected_hash"
    error "Получено:  $actual_hash"
    log_install "Error: SHA-256 mismatch for xcp_${ver}_${ARCH_LABEL}"
    return 1
  fi
  
  ok "SHA-256 контрольная сумма совпадает"
  rm -f "$cs_file"
  return 0
}

# Миграция со старого имени xkeen-control-panel
do_migration() {
  local OLD_DIR="/opt/etc/xkeen-control-panel"
  local OLD_BIN="/opt/bin/xkeen-control-panel"
  local OLD_INIT="/opt/etc/init.d/S99xkeen-control-panel"
  
  if [ -d "$OLD_DIR" ] || [ -f "$OLD_BIN" ] || [ -f "$OLD_INIT" ]; then
    info "Обнаружена старая установка. Начинаем миграцию в $INSTALL_DIR..."
    log_install "Start migration process"
    
    # 1. Останавливаем старый сервис
    if [ -f "$OLD_INIT" ]; then
      "$OLD_INIT" stop 2>/dev/null || true
    fi
    killall -q "xkeen-control-panel" 2>/dev/null || true
    
    # 2. Переносим конфигурацию и бэкапы
    mkdir -p "$INSTALL_DIR"
    if [ -f "$OLD_DIR/config.json" ]; then
      # Мигрируем config.json с заменой data_dir
      sed 's|/opt/etc/xkeen-control-panel|/opt/etc/xcp|g' "$OLD_DIR/config.json" > "$INSTALL_DIR/config.json"
      ok "Конфигурация config.json перенесена и обновлена"
      log_install "Migrated config.json with updated data_dir"
    fi
    
    if [ -d "$OLD_DIR/backup" ]; then
      cp -r "$OLD_DIR/backup" "$INSTALL_DIR/"
      ok "Бэкапы перенесены"
      log_install "Migrated backup directory"
    fi

    if [ -d "$OLD_DIR/ssl" ]; then
      cp -r "$OLD_DIR/ssl" "$INSTALL_DIR/"
      ok "SSL сертификаты перенесены"
      log_install "Migrated ssl directory"
    fi
    
    # 3. Удаляем старые файлы
    rm -rf "$OLD_DIR"
    rm -f "$OLD_BIN"
    rm -f "$OLD_INIT"
    ok "Миграция старых файлов завершена"
    log_install "Cleaned up old installation artifacts"
  fi
}

# Опрос API для проверки доступности
poll_api() {
  local url="http://127.0.0.1:${_port}/api/auth/me"
  # Если в конфиге включен HTTPS, то пробуем https. Будем проверять с помощью флагов -k / --no-check-certificate
  local count=1
  local max_tries=3
  info "Проверяем доступность API по адресу $url..."
  
  while [ $count -le $max_tries ]; do
    if curl -k -fsL "$url" >/dev/null 2>&1 || wget --no-check-certificate -qO- "$url" >/dev/null 2>&1; then
      ok "API успешно отвечает"
      log_install "API polling succeeded on try $count"
      return 0
    fi
    # Попытка проверить https
    local https_url="https://127.0.0.1:${_port}/api/auth/me"
    if curl -k -fsL "$https_url" >/dev/null 2>&1 || wget --no-check-certificate -qO- "$https_url" >/dev/null 2>&1; then
      ok "API успешно отвечает (HTTPS)"
      log_install "API polling succeeded on try $count (HTTPS)"
      return 0
    fi
    
    warn "Попытка $count из $max_tries: API недоступен, ждем..."
    sleep 3
    count=$((count + 1))
  done
  
  error "API не ответил после $max_tries попыток"
  log_install "Error: API not responding after $max_tries attempts"
  return 1
}

# Загрузка бинарника с проверкой и ретриями
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
  
  # Проверка версии
  local cur_ver=$(get_version)
  if [ "$cur_ver" = "$LATEST_VER" ]; then
    ok "Уже установлена последняя версия: $LATEST_VER"
    log_install "Already up to date: version $LATEST_VER"
    return 2
  fi

  # 1. Основной источник — GitHub Releases
  info "Пробуем GitHub Releases..."
  _url=$(get_github_releases_url "$LATEST_VER")
  if try_download "$_url"; then
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
  _url=$(get_jsdelivr_url "$LATEST_VER")
  if try_download "$_url"; then
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
  _url=$(get_raw_url "$LATEST_VER")
  if try_download "$_url"; then
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
 ▒▒███ ███   ▒███  ███     ██████   ██████  ████████      ███     ▒▒▒  ▒███    ▒███
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

  detect_arch
  do_migration
  create_config
  
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
  
  local _port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  poll_api || return 1

  _ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  _ip=${_ip:-"<IP-роутера>"}

  printf "\n${GREEN}${BOLD}========================================${NC}\n"
  printf "${GREEN}  Установка завершена!${NC}\n"
  printf "${GREEN}${BOLD}========================================${NC}\n"
  printf "  Версия:  %s\n" "$(get_version)"
  printf "  Веб-UI:  http://%s:%s\n" "$_ip" "$_port"
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
    return
  fi

  _old_version=$(get_version)
  info "Обновление с ${_old_version} (${CHANNEL})..."
  log_install "Starting update from $_old_version, channel: $CHANNEL"

  detect_arch
  
  install_binary
  local install_status=$?
  if [ $install_status -eq 1 ]; then
    error "Обновление прервано"
    return 1
  fi
  
  if [ $install_status -ne 2 ]; then
    stop_service
    start_service
    _new_version=$(get_version)
    ok "Обновлено: ${_old_version} → ${_new_version}"
    log_install "Updated successfully: $_old_version -> $_new_version"
  fi
  
  local _port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  poll_api || return 1
  
  info "Обновите страницу в браузере: Ctrl+Shift+R"
}

# Удаление
do_uninstall() {
  if [ ! -f "$BIN_PATH" ] && [ ! -f "$INIT_SCRIPT" ]; then
    error "Панель не установлена."
    return
  fi

  printf "\n${RED}${BOLD}Будет удалена панель и все её файлы.${NC}\n"
  printf "Продолжить? [y/N]: "
  read response < /dev/tty
  case "$response" in
    [Yy]) ;;
    *) info "Отменено"; return ;;
  esac

  stop_service
  rm -f "$BIN_PATH"
  rm -f "$INIT_SCRIPT"

  printf "\nУдалить директорию конфигов (%s)? [y/N]: " "$INSTALL_DIR"
  read response < /dev/tty
  case "$response" in
    [Yy]) rm -rf "$INSTALL_DIR"; ok "Конфиги удалены" ;;
    *) ok "Конфиги сохранены" ;;
  esac

  ok "Удаление завершено"
}

# Главное меню
show_menu() {
  _version=$(get_version)
  printf "\n"
  printf "  Архитектура: ${GREEN}%s${NC}\n" "$ARCH_LABEL"
  printf "  Версия:      ${GREEN}%s${NC}\n" "$_version"
  printf "  Канал:       ${YELLOW}%s${NC}\n" "$([ "$CHANNEL" = "stable" ] && echo "Stable (стабильный)" || echo "Pre-release (тестовый)")"
  printf "\n"
  printf "  ${BOLD}Действия:${NC}\n"
  printf "    1. Установить / Переустановить\n"
  printf "    2. Обновить\n"
  printf "    3. Удалить\n"
  printf "\n"
  printf "  ${BOLD}Канал:${NC}\n"
  printf "    9. Переключить на %s\n" "$([ "$CHANNEL" = "stable" ] && echo "Pre-release" || echo "Stable")"
  printf "\n"
  printf "    0. Выход\n\n"
  printf "${GREEN}> ${NC}"
}

# ===== Главный цикл =====

detect_arch

# Если передан аргумент — выполняем команду без меню
if [ -n "$1" ]; then
  case "$1" in
    install|i)   CHANNEL="stable"; do_install; exit 0 ;;
    update|u)    CHANNEL="stable"; do_update; exit 0 ;;
    uninstall|r) do_uninstall; exit 0 ;;
    *) error "Неизвестная команда: $1"; exit 1 ;;
  esac
fi

# Пробуем интерактивное меню через /dev/tty
if [ -r /dev/tty ]; then
  CHANNEL="stable"

  while true; do
    print_banner
    show_menu
    read choice < /dev/tty || {
      warn "Терминал недоступен, переключаемся в автоматический режим"
      do_install
      exit 0
    }

    case "$choice" in
      1) do_install ;;
      2) do_update ;;
      3) do_uninstall ;;
      9)
        if [ "$CHANNEL" = "stable" ]; then
          CHANNEL="prerelease"
          ok "Канал: Pre-release (тестовые сборки)"
        else
          CHANNEL="stable"
          ok "Канал: Stable (стабильные сборки)"
        fi
        ;;
      0) ok "До свидания!"; exit 0 ;;
      *) error "Неверный выбор" ;;
    esac

    printf "\nНажмите Enter для продолжения..."
    read dummy < /dev/tty || true
  done
else
  info "Запущено в неинтерактивном режиме. Устанавливаем stable..."
  CHANNEL="stable"
  do_install
fi
