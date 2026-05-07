#!/bin/sh
set -e

REPO="shisui1511/xkeen-control-panel"
BINARY="xkeen-control-panel"
INSTALL_DIR="/opt/etc/xkeen-control-panel"
BIN_PATH="/opt/bin/xkeen-control-panel"
INIT_SCRIPT="/opt/etc/init.d/S99xkeen-control-panel"
DEFAULT_PORT=8089
BACKUP_PATH="/opt/etc/xkeen-control-panel/backup"

CHANNEL="stable"

# Colors
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
CYAN='\033[36m'
NC='\033[0m'

info()  { printf "${CYAN}ℹ  %s${NC}\n" "$1"; }
ok()    { printf "${GREEN}✅ %s${NC}\n" "$1"; }
warn()  { printf "${YELLOW}⚠  %s${NC}\n" "$1"; }
error() { printf "${RED}❌ %s${NC}\n" "$1"; }

# Detect system language
detect_lang() {
  # Try to get locale from system
  SYS_LANG="en"
  if command -v locale >/dev/null 2>&1; then
    LOCALE=$(locale -k LC_MESSAGES 2>/dev/null | grep -o 'ru_RU\|ru' || echo "")
    if [ -n "$LOCALE" ]; then
      SYS_LANG="ru"
    fi
  fi
  # Check environment variables
  if [ -n "$LANG" ]; then
    case "$LANG" in
      ru_*|ru) SYS_LANG="ru" ;;
    esac
  fi
  if [ -n "$LC_ALL" ]; then
    case "$LC_ALL" in
      ru_*|ru) SYS_LANG="ru" ;;
    esac
  fi
  echo "$SYS_LANG"
}

# Detect architecture
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
      error "Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac
}

# Get download URL
get_download_url() {
  echo "https://github.com/${REPO}/releases/latest/download/${BINARY}-linux-${ARCH_LABEL}"
}

# Stop running service
stop_service() {
  if [ -f "$INIT_SCRIPT" ]; then
    info "Stopping service..."
    "$INIT_SCRIPT" stop 2>/dev/null || killall -q "$BINARY" 2>/dev/null || true
    sleep 1
  fi
}

# Start service
start_service() {
  if [ -f "$INIT_SCRIPT" ]; then
    info "Starting service..."
    "$INIT_SCRIPT" start 2>/dev/null || true
    sleep 1
  fi
}

# Backup current binary
backup_current() {
  if [ -f "$BIN_PATH" ]; then
    mkdir -p "$BACKUP_PATH"
    cp "$BIN_PATH" "$BACKUP_PATH/${BINARY}.bak.$(date +%Y%m%d%H%M%S)"
    ok "Backup created"
  fi
}

# Download and install binary
install_binary() {
  DOWNLOAD_URL=$(get_download_url)
  info "Downloading from: $DOWNLOAD_URL"

  mkdir -p "$INSTALL_DIR"
  mkdir -p "$(dirname "$BIN_PATH")"

  # Download to temp first, then move atomically
  TEMP_BIN="/tmp/${BINARY}.new"
  if command -v curl >/dev/null 2>&1; then
    curl -fL -o "$TEMP_BIN" "$DOWNLOAD_URL"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TEMP_BIN" "$DOWNLOAD_URL"
  else
    error "Neither curl nor wget found"
    exit 1
  fi

  chmod +x "$TEMP_BIN"
  mv "$TEMP_BIN" "$BIN_PATH"
  ok "Binary installed"
}

# Create default config
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
    ok "Created default config: $CONFIG_FILE"
  fi
}

# Create init script
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
  ok "Init script created"
}

# Check port availability
check_port() {
  DETECTED_PORT=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  info "Checking port $DETECTED_PORT..."

  if command -v netstat >/dev/null 2>&1; then
    if netstat -tln 2>/dev/null | grep -q ":${DETECTED_PORT} "; then
      warn "Port $DETECTED_PORT is already in use"
    fi
  elif command -v ss >/dev/null 2>&1; then
    if ss -tln 2>/dev/null | grep -q ":${DETECTED_PORT} "; then
      warn "Port $DETECTED_PORT is already in use"
    fi
  fi
}

# Get current version
get_version() {
  if [ -f "$BIN_PATH" ]; then
    timeout 2 "$BIN_PATH" -v 2>/dev/null | awk '{print $NF}' || echo "unknown"
  else
    echo "not installed"
  fi
}

# Print banner
print_banner() {
  printf "${CYAN}"
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

# Finish message
finish_message() {
  local action="$1"
  local ip=$(ip -4 a s br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p')
  ip=${ip:-"<router-ip>"}
  local port=$(grep -o '"port":[[:space:]]*[0-9]*' "$INSTALL_DIR/config.json" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")
  local version=$(get_version)

  printf "\n${GREEN}=========================================="
  printf "\n  XKeen Control Panel ${action}!"
  printf "\n==========================================${NC}\n"
  printf "  Version: %s\n" "$version"
  printf "  Web UI:  http://%s:%s\n" "$ip" "$port"
  printf "  Config:  %s/config.json\n" "$INSTALL_DIR"
  printf "\n  Start:   %s start\n" "$INIT_SCRIPT"
  printf "  Stop:    %s stop\n" "$INIT_SCRIPT"
  printf "  Status:  %s status\n" "$INIT_SCRIPT"
  printf "${GREEN}==========================================${NC}\n\n"
}

# Install action
do_install() {
  info "Installing XKeen Control Panel ($CHANNEL)..."

  detect_arch
  create_config
  check_port
  install_binary
  create_init_script
  start_service

  finish_message "installed"
}

# Update action
do_update() {
  if [ ! -f "$BIN_PATH" ]; then
    error "Not installed. Run install first."
    exit 1
  fi

  local old_version=$(get_version)
  info "Updating from v$old_version ($CHANNEL)..."

  detect_arch
  stop_service
  backup_current
  install_binary
  start_service

  local new_version=$(get_version)
  ok "Updated: v$old_version → v$new_version"

  finish_message "updated"
}

# Uninstall action
do_uninstall() {
  printf "\n${RED}This will REMOVE XKeen Control Panel and its files.${NC}\n"
  printf "Continue? [y/N]: "
  read response
  case "$response" in
    [Yy]) ;;
    *) info "Cancelled"; exit 0 ;;
  esac

  stop_service
  rm -f "$BIN_PATH"
  rm -f "$INIT_SCRIPT"

  printf "\nRemove config directory? [y/N]: "
  read response
  case "$response" in
    [Yy]) rm -rf "$INSTALL_DIR"; ok "Config removed" ;;
    *) ok "Config kept at $INSTALL_DIR" ;;
  esac

  ok "Uninstall complete"
}

# Main
detect_arch
print_banner

CURRENT_VERSION=$(get_version)
printf "  Architecture: ${GREEN}%s${NC}\n" "$ARCH_LABEL"
printf "  Version:      ${GREEN}%s${NC}\n\n" "$CURRENT_VERSION"

# If argument provided, run action directly
case "$1" in
  install) do_install; exit 0 ;;
  update) do_update; exit 0 ;;
  uninstall) do_uninstall; exit 0 ;;
esac

# Interactive menu
printf "Choose action:\n"
printf "  1. Install / Reinstall\n"
printf "  2. Update\n"
printf "  3. Uninstall\n"
printf "  0. Exit\n\n"
printf "${GREEN}> ${NC}"
read choice

case "$choice" in
  1) do_install ;;
  2) do_update ;;
  3) do_uninstall ;;
  0) exit 0 ;;
  *) error "Invalid choice"; exit 1 ;;
esac
