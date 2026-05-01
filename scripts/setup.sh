#!/bin/sh
set -e

REPO="shisui1511/xkeen-control-panel"
BINARY="xkeen-control-panel"
INSTALL_DIR="/opt/etc/xkeen-control-panel"
BIN_PATH="/opt/bin/xkeen-control-panel"
DEFAULT_PORT=8089

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
	mips|mipsel|mipsle)
		BIN_URL="https://github.com/${REPO}/releases/latest/download/${BINARY}-linux-mipsle"
		;;
	aarch64|arm64)
		BIN_URL="https://github.com/${REPO}/releases/latest/download/${BINARY}-linux-arm64"
		;;
	*)
		echo "Unsupported architecture: $ARCH"
		exit 1
		;;
esac

echo "Installing XKeen Control Panel..."

# Create directories
mkdir -p "$INSTALL_DIR"
mkdir -p "$(dirname $BIN_PATH)"

# Download binary
echo "Downloading from $BIN_URL..."
curl -fL -o "$BIN_PATH" "$BIN_URL" || {
	wget -qO "$BIN_PATH" "$BIN_URL"
}

chmod +x "$BIN_PATH"

# Create default config if not exists
CONFIG_FILE="$INSTALL_DIR/config.json"
if [ ! -f "$CONFIG_FILE" ]; then
	cat > "$CONFIG_FILE" <<EOF
{
  "port": $DEFAULT_PORT,
  "xray_config_dir": "/opt/etc/xray/configs",
  "xkeen_binary": "xkeen",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "mihomo_binary": "mihomo",
  "data_dir": "$INSTALL_DIR"
}
EOF
	echo "Created default config: $CONFIG_FILE"
fi

# Detect current port from config (fallback to default)
DETECTED_PORT=$(grep -o '"port":[[:space:]]*[0-9]*' "$CONFIG_FILE" 2>/dev/null | grep -o '[0-9]*' || echo "$DEFAULT_PORT")

# Check port availability
echo "Checking port $DETECTED_PORT..."
if command -v netstat >/dev/null 2>&1; then
	if netstat -tln 2>/dev/null | grep -q ":${DETECTED_PORT} "; then
		echo "WARNING: Port $DETECTED_PORT is already in use."
		echo "You may need to change port in $CONFIG_FILE or stop the conflicting service."
		echo "Common conflicts:"
		echo "  - XKeen-UI (umarcheh001) may use ports 8088, 8091, 8100-8199"
		echo "  - Mihomo Clash API usually uses 9090"
		echo "  - zashboard is a static UI and does not occupy a port"
	fi
elif command -v ss >/dev/null 2>&1; then
	if ss -tln 2>/dev/null | grep -q ":${DETECTED_PORT} "; then
		echo "WARNING: Port $DETECTED_PORT is already in use."
	fi
fi

# Create init script
cat > /opt/etc/init.d/S99xkeen-control-panel <<EOF
#!/bin/sh
ENABLED=yes
PROCS=xkeen-control-panel
ARGS="-config $INSTALL_DIR/config.json"
PREARGS=""
DESC="XKeen Control Panel"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

. /opt/etc/init.d/rc.func
EOF

chmod +x /opt/etc/init.d/S99xkeen-control-panel

echo ""
echo "=========================================="
echo "Installation complete!"
echo "=========================================="
echo "Start:   /opt/etc/init.d/S99xkeen-control-panel start"
echo "Stop:    /opt/etc/init.d/S99xkeen-control-panel stop"
echo "Status:  /opt/etc/init.d/S99xkeen-control-panel status"
echo "Web UI:  http://<router-ip>:$DETECTED_PORT"
echo ""
echo "Config:  $CONFIG_FILE"
echo ""
echo "Coexistence notes:"
echo "  - This panel works alongside umarcheh001/XKeen-UI and zashboard"
echo "  - To avoid port conflicts, ensure no other panel uses port $DETECTED_PORT"
echo "  - zashboard is a static UI for Mihomo and does not occupy a port"
echo "    (it is served via Mihomo's external-ui feature)"
echo "=========================================="
