#!/bin/bash
# macOS post-install script for LocalWEB .pkg
# Runs after installation to configure daemon, permissions, and firewall

set -e

APP_PATH="/Applications/LocalWEB.app"
DAEMON_PLIST="/Library/LaunchDaemons/com.localweb.daemon.plist"
DATA_DIR="/Library/Application Support/LocalWEB"
LOG_DIR="/var/log"

echo "LocalWEB post-install configuration..."

# Create data directory
mkdir -p "$DATA_DIR"
chown root:wheel "$DATA_DIR"
chmod 750 "$DATA_DIR"

# Create log files
touch "$LOG_DIR/localweb.log" "$LOG_DIR/localweb.error.log"
chown root:wheel "$LOG_DIR/localweb.log" "$LOG_DIR/localweb.error.log"
chmod 644 "$LOG_DIR/localweb.log" "$LOG_DIR/localweb.error.log"

# Copy daemon plist
cp "$APP_PATH/Contents/Resources/LaunchDaemon.plist" "$DAEMON_PLIST"
chown root:wheel "$DAEMON_PLIST"
chmod 644 "$DAEMON_PLIST"

# Load daemon
launchctl load "$DAEMON_PLIST" 2>/dev/null || true
sleep 2

# Verify daemon is running
if launchctl list | grep -q "com.localweb.daemon"; then
    echo "LocalWEB daemon loaded successfully"
else
    echo "WARNING: Daemon may not have started correctly"
fi

# Configure firewall (requires user approval)
echo "Configuring firewall..."
/usr/libexec/ApplicationFirewall/socketfilterfw --add "/Applications/LocalWEB.app/Contents/MacOS/localweb" 2>/dev/null || true
/usr/lib/sbin/socketfilterfw --setglobalstate on 2>/dev/null || true

# Request Network Extension entitlement for VPN (requires user approval)
echo ""
echo "IMPORTANT: For VPN functionality, you must:"
echo "1. Open System Settings > Privacy & Security"
echo "2. Allow 'LocalWEB' under 'Network' and 'Local Network'"
echo "3. For VPN, approve the Network Extension in System Settings > General > VPN & Network"
echo ""

# Request Bluetooth permission
echo "For Bluetooth peer discovery:"
echo "1. Open System Settings > Privacy & Security > Bluetooth"
echo "2. Enable 'LocalWEB'"
echo ""

# Create CLI symlink
ln -sf "/Applications/LocalWEB.app/Contents/MacOS/localweb-cli" "/usr/local/bin/localweb-cli" 2>/dev/null || true

echo "LocalWEB post-install configuration complete"
echo ""
echo "To start the daemon manually: sudo launchctl load $DAEMON_PLIST"
echo "To view logs: tail -f /var/log/localweb.log"
echo ""
exit 0