#!/bin/sh

REPO="shisui1511/xkeen-control-panel"
BINARY="xkeen-control-panel"
INSTALL_DIR="/opt/etc/xkeen-control-panel"
BIN_PATH="/opt/bin/xkeen-control-panel"
INIT_SCRIPT="/opt/etc/init.d/S99xkeen-control-panel"
DEFAULT_PORT=8089

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
# uname -m может врать (на mipsel роутерах иногда возвращает 'mips')
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

# Fallback URLs для скачивания бинарников
# Имена файлов: xcp_{VERSION}_{ARCH}
# 1. GitHub Releases (primary)
# 2. jsDelivr CDN (fallback)
# 3. raw.githubusercontent.com (fallback)

# Получить latest stable версию
get_latest_stable_version() {
  _json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases/latest" || echo "")
  if [ -n "$_json" ]; then
    _tag=$(echo "$_json" | grep -m1 '"tag_name"' | sed 's/.*"\([^"]*\)"$/\1/')
    echo "$_tag"
  else
    echo ""
  fi
}

# Получить latest pre-release версию
# GitHub API возвращает releases в произвольном порядке (не по дате).
# Загружаем больше релизов, извлекаем все теги, сортируем по версии и берём максимальный.
get_latest_prerelease_version() {
  _json=$(curl -s --connect-timeout 5 --max-time 10 "https://api.github.com/repos/${REPO}/releases?per_page=30" || echo "")
  if [ -n "$_json" ]; then
    # Извлекаем все tag_name и сортируем по версии (sort -V)
    _tag=$(echo "$_json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | sort -V | tail -1)
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

# Остановка сервиса
stop_service() {
  if [ -f "$INIT_SCRIPT" ]; then
    info "Останавливаем сервис..."
    "$INIT_SCRIPT" stop 2>/dev/null || killall -q "$BINARY" 2>/dev/null || true
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
    "enabled": false,
    "cert_path": "",
    "key_path": ""
  }
}
EOF
    ok "Конфиг создан: $CONFIG_FILE"
  fi
}

# Создание init-скрипта
create_init_script() {
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

# Загрузка бинарника с fallback chain
install_binary() {
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$(dirname "$BIN_PATH")"
  TEMP_BIN="/tmp/${BINARY}.new"

  # Получаем версию в зависимости от канала
  info "Определяем версию..."
  LATEST_VER=$(get_release_version)
  if [ -z "$LATEST_VER" ]; then
    error "Не удалось определить версию"
    return 1
  fi
  info "Версия: $LATEST_VER"

  # 1. Основной источник — GitHub Releases
  info "Пробуем GitHub Releases..."
  _url=$(get_github_releases_url "$LATEST_VER")
  if try_download "$_url"; then
    ok "Загружено с GitHub Releases"
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    return 0
  fi

  # 2. Fallback — jsDelivr CDN
  warn "GitHub недоступен, пробуем jsDelivr..."
  _url=$(get_jsdelivr_url "$LATEST_VER")
  if try_download "$_url"; then
    ok "Загружено через jsDelivr"
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    return 0
  fi

  # 3. Fallback — raw.githubusercontent.com
  warn "jsDelivr недоступен, пробуем raw.githubusercontent.com..."
  _url=$(get_raw_url "$LATEST_VER")
  if try_download "$_url"; then
    ok "Загружено с raw.githubusercontent.com"
    chmod +x "$TEMP_BIN"
    mv "$TEMP_BIN" "$BIN_PATH"
    ok "Бинарник установлен"
    return 0
  fi

  error "Все источники недоступны. Проверьте интернет или установите вручную:"
  info "https://github.com/${REPO}/releases"
  return 1
}

# Текущая версия
get_version() {
  if [ -f "$BIN_PATH" ]; then
    "$BIN_PATH" -version 2>/dev/null || echo "неизвестна"
  else
    echo "не установлена"
  fi
}

# Баннер
print_banner() {
  printf "${CYAN}${BOLD}"
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

  detect_arch
  create_config
  install_binary || return
  create_init_script
  start_service

  _ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  _ip=${_ip:-"<IP-роутера>"}
  _port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")

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
}

# Обновление
do_update() {
  if [ ! -f "$BIN_PATH" ]; then
    error "Панель не установлена. Сначала установите."
    return
  fi

  _old_version=$(get_version)
  info "Обновление с ${_old_version} (${CHANNEL})..."

  detect_arch
  stop_service
  install_binary || {
    start_service
    return
  }
  start_service

  _new_version=$(get_version)
  ok "Обновлено: ${_old_version} → ${_new_version}"
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
  # Нет терминала — автоустановка stable
  info "Запущено в неинтерактивном режиме. Устанавливаем stable..."
  CHANNEL="stable"
  do_install
fi
