#!/bin/sh
set -e

REPO="user/xkeen-control-panel"
BINARY="xkeen-control-panel"
INSTALL_DIR="/opt/etc/xkeen-control-panel"
BIN_PATH="/opt/bin/xkeen-control-panel"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
	mips)
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

echo "Installation complete!"
echo "Start service: /opt/etc/init.d/S99xkeen-control-panel start"
echo "Web UI: http://<router-ip>:8089"
