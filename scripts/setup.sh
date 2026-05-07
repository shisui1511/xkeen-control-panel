#!/bin/sh
set -e

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
detect_arch() {
  ARCH=$(uname -m)
  case "$ARCH" in
    mips|mipsel|mipsle)
      ARCH_LABEL="mipsle"
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

# URL для загрузки (stable или pre-release)
get_download_url() {
  if [ "$CHANNEL" = "prerelease" ]; then
    # Берём последний pre-release
    PREREL_TAG=$(curl -s "https://api.github.com/repos/${REPO}/releases" | \
      grep -m1 '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | grep -E '\-dev\.|\-alpha\.' || echo "")
    if [ -z "$PREREL_TAG" ]; then
      warn "Pre-release не найден, используем stable"
      CHANNEL="stable"
    else
      echo "https://github.com/${REPO}/releases/download/${PREREL_TAG}/${BINARY}-linux-${ARCH_LABEL}"
      return
    fi
  fi
  echo "https://github.com/${REPO}/releases/latest/download/${BINARY}-linux-${ARCH_LABEL}"
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
  "lang": "$SYS_LANG"
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

# Загрузка бинарника
install_binary() {
  DOWNLOAD_URL=$(get_download_url)
  info "Загружаем: $DOWNLOAD_URL"

  mkdir -p "$INSTALL_DIR"
  mkdir -p "$(dirname "$BIN_PATH")"

  TEMP_BIN="/tmp/${BINARY}.new"
  if command -v curl >/dev/null 2>&1; then
    curl -fL -o "$TEMP_BIN" "$DOWNLOAD_URL"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TEMP_BIN" "$DOWNLOAD_URL"
  else
    error "Не найден curl или wget"
    exit 1
  fi

  chmod +x "$TEMP_BIN"
  mv "$TEMP_BIN" "$BIN_PATH"
  ok "Бинарник установлен"
}

# Текущая версия
get_version() {
  if [ -f "$BIN_PATH" ]; then
    timeout 2 "$BIN_PATH" -v 2>/dev/null | awk '{print $NF}' || echo "неизвестна"
  else
    echo "не установлена"
  fi
}

# Баннер
print_banner() {
  printf "${CYAN}${BOLD}"
  cat <<'EOF'
╔═══════════════════════════════════════╗
║   XKeen Control Panel                 ║
║   Панель управления XKeen/Mihomo      ║
╚═══════════════════════════════════════╝
EOF
  printf "${NC}\n"
}

# Установка
do_install() {
  info "Установка XKeen Control Panel ($CHANNEL)..."

  detect_arch
  create_config
  install_binary
  create_init_script
  start_service

  local ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  ip=${ip:-"<IP-роутера>"}
  local port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")

  printf "\n${GREEN}${BOLD}========================================${NC}\n"
  printf "${GREEN}  Установка завершена!${NC}\n"
  printf "${GREEN}${BOLD}========================================${NC}\n"
  printf "  Версия:  %s\n" "$(get_version)"
  printf "  Веб-UI:  http://%s:%s\n" "$ip" "$port"
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

  local old_version=$(get_version)
  info "Обновление с v%s (%s)..." "$old_version" "$CHANNEL"

  detect_arch
  stop_service
  install_binary
  start_service

  local new_version=$(get_version)
  ok "Обновлено: v%s → v%s" "$old_version" "$new_version"
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
  read response
  case "$response" in
    [Yy]) ;;
    *) info "Отменено"; return ;;
  esac

  stop_service
  rm -f "$BIN_PATH"
  rm -f "$INIT_SCRIPT"

  printf "\nУдалить директорию конфигов (%s)? [y/N]: " "$INSTALL_DIR"
  read response
  case "$response" in
    [Yy]) rm -rf "$INSTALL_DIR"; ok "Конфиги удалены" ;;
    *) ok "Конфиги сохранены" ;;
  esac

  ok "Удаление завершено"
}

# Главное меню
show_menu() {
  local version=$(get_version)
  printf "\n"
  printf "  Архитектура: ${GREEN}%s${NC}\n" "$ARCH_LABEL"
  printf "  Версия:      ${GREEN}%s${NC}\n" "$version"
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

# Если аргумент — команда
if [ -n "$1" ]; then
  case "$1" in
    install|i)   CHANNEL="stable"; do_install; exit 0 ;;
    update|u)    CHANNEL="stable"; do_update; exit 0 ;;
    uninstall|r) do_uninstall; exit 0 ;;
    *) error "Неизвестная команда: $1"; exit 1 ;;
  esac
fi

CHANNEL="stable"

while true; do
  print_banner
  show_menu
  read choice

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
  read dummy
  clear
done
