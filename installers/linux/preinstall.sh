#!/bin/bash
# LocalWEB pre-install script for Debian/Ubuntu
# Runs before package installation

set -e

echo "LocalWEB pre-install: checking prerequisites..."

# Check for systemd
if ! command -v systemctl >/dev/null 2>&1; then
    echo "ERROR: systemd is required but not found"
    exit 1
fi

# Check kernel version (need 5.10+ for WireGuard)
KERNEL_VERSION=$(uname -r | cut -d. -f1,2)
KERNEL_MAJOR=$(echo $KERNEL_VERSION | cut -d. -f1)
KERNEL_MINOR=$(echo $KERNEL_VERSION | cut -d. -f2)

if [ "$KERNEL_MAJOR" -lt 5 ] || ([ "$KERNEL_MAJOR" -eq 5 ] && [ "$KERNEL_MINOR" -lt 10 ]); then
    echo "WARNING: Kernel version $KERNEL_VERSION detected. LocalWEB VPN works best with kernel 5.10+"
fi

# Check for required capabilities
if ! grep -q "CAP_NET_ADMIN" /proc/self/status 2>/dev/null; then
    echo "INFO: CAP_NET_ADMIN capability will be needed for VPN"
fi

# Check for existing localweb user/group
if ! getent group localweb >/dev/null 2>&1; then
    echo "Creating localweb group..."
    groupadd --system localweb 2>/dev/null || true
fi

if ! getent passwd localweb >/dev/null 2>&1; then
    echo "Creating localweb user..."
    useradd --system --home-dir /var/lib/localweb --shell /usr/sbin/nologin --gid localweb localweb 2>/dev/null || true
fi

# Check for existing installation
if systemctl is-active --quiet localweb 2>/dev/null; then
    echo "Stopping existing LocalWEB service..."
    systemctl stop localweb 2>/dev/null || true
    systemctl disable localweb 2>/dev/null || true
fi

# Check for port conflicts
if ss -tuln | grep -q ":4443 "; then
    echo "WARNING: Port 4443 appears to be in use"
fi

# Create necessary directories
mkdir -p /etc/localweb
mkdir -p /var/lib/localweb
mkdir -p /var/log/localweb
mkdir -p /var/run/localweb

# Set permissions
chown -R localweb:localweb /var/lib/localweb /var/log/localweb /var/run/localweb 2>/dev/null || true
chmod 750 /var/lib/localweb /var/log/localweb /var/run/localweb 2>/dev/null || true

echo "Pre-install completed successfully"
exit 0