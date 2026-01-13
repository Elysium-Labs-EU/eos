#!/bin/bash
set -e

if [ $EUID -ne 0 ]; then
    echo "Script needs to be run as root"
    exit 1
fi


if command -v curl &> /dev/null; then
    DOWNLOAD_CMD="curl -sSL"
    DOWNLOAD_OUTPUT="-o"
elif command -v wget &> /dev/null; then
    DOWNLOAD_CMD="wget -qO"
    DOWNLOAD_OUTPUT=""
else
    echo "Error: Neither curl nor wget is installed."
    echo "Please install one of them:"
    echo "  Debian/Ubuntu: apt-get install curl"
    echo "  RHEL/CentOS:   yum install curl"
    exit 1
fi

ARCH=$(uname -m)

case $ARCH in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

VERSION="${EOS_VERSION:-}"

if [ -z "$VERSION" ]; then
    echo "Fetching latest version..."
    if command -v curl &> /dev/null; then
        VERSION=$(curl -sSL "https://api.github.com/repos/Elysium-Labs-EU/eos/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    else
        VERSION=$(wget -qO- "https://api.github.com/repos/Elysium-Labs-EU/eos/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    fi
    
    if [ -z "$VERSION" ]; then
        echo "Failed to fetch latest version"
        exit 1
    fi
    echo "Latest version: $VERSION"
fi

echo "Downloading eos $VERSION for linux-$ARCH..."

DOWNLOAD_URL="https://github.com/Elysium-Labs-EU/eos/releases/download/$VERSION/eos-linux-$ARCH"

if command -v curl &> /dev/null; then
    curl -L -o /tmp/eos "$DOWNLOAD_URL"
else
    wget -O /tmp/eos "$DOWNLOAD_URL"
fi

if [ $? -ne 0 ]; then
    echo "Download failed"
    echo "Tried URL: $DOWNLOAD_URL"
    exit 1
fi

if [ ! -f /tmp/eos ]; then
    echo "Binary not found after download"
    exit 1
fi

echo "Installing binary..."
chmod +x /tmp/eos
cp /tmp/eos /usr/local/bin/eos

echo "Binary installed to /usr/local/bin/eos"

echo "Installing SQLite3..."
if command -v apt-get &> /dev/null; then
    apt-get update -qq && apt-get install -y -qq sqlite3 > /dev/null 2>&1
elif command -v yum &> /dev/null; then
    yum install -y -q sqlite > /dev/null 2>&1
elif command -v apk &> /dev/null; then
    apk add --quiet sqlite > /dev/null 2>&1
else
    echo "Warning: Could not install sqlite3 automatically."
    echo "Please install it manually to query your data:"
    echo "  Debian/Ubuntu: apt-get install sqlite3"
    echo "  RHEL/CentOS:   yum install sqlite"
    echo "  Alpine:        apk add sqlite"
fi

echo "Creating eos home directory..."
mkdir -p /root/.eos

echo "Installing systemd service..."
cat > /etc/systemd/system/eos.service << 'EOF'
[Unit]
Description=Eos - Service Orchestration Tool
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/.eos
ExecStart=/usr/local/bin/eos daemon
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=false
ReadWritePaths=/root/.eos

[Install]
WantedBy=multi-user.target
EOF

echo "Enabling eos service..."
systemctl daemon-reload
systemctl enable eos.service

echo ""
echo "eos installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Register a service:   eos add /path/to/project"
echo "  2. Start the daemon:     sudo systemctl start eos"
echo "  3. Check status:         sudo systemctl status eos"
echo "  4. View logs:            sudo journalctl -u eos -f"
echo "  5. List services:        eos status"
echo ""
echo "Database location: ~/.eos/state.db"
echo "Service configs:   <project-path>/service.yaml"
echo ""