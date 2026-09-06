#!/bin/bash
# LocalWEB pre-install script for RPM (RHEL/Fedora)

set -e

echo "LocalWEB RPM pre-install: checking prerequisites..."

# Check for systemd
if ! command -v systemctl >/dev/null 2>&1; then
    echo "ERROR: systemd is required but not found"
    exit 1
fi

# Check kernel version
KERNEL_VERSION=$(uname -r | cut -d. -f1,2)
KERNEL_MAJOR=$(echo $KERNEL_VERSION | cut -d. -f1)
KERNEL_MINOR=$(echo $KERNEL_VERSION | cut -d. -f2)

if [ "$KERNEL_MAJOR" -lt 5 ] || ([ "$KERNEL_MAJOR" -eq 5 ] && [ "$KERNEL_MINOR" -lt 10 ]); then
    echo "WARNING: Kernel version $KERNEL_VERSION detected. LocalWEB VPN works best with kernel 5.10+"
fi

# Create localweb user/group
getent group localweb >/dev/null 2>&1 || groupadd -r localweb 2>/dev/null || true
getent passwd localweb >/dev/null 2>&1 || useradd -r -d /var/lib/localweb -s /sbin/nologin -g localweb localweb 2>/dev/null || true

# Stop existing service
systemctl stop localweb 2>/dev/null || true
systemctl disable localweb 2>/dev/null || true

# Create directories
mkdir -p /etc/localweb
mkdir -p /var/lib/localweb
mkdir -p /var/log/localweb
mkdir -p /var/run/localweb

chown -R localweb:localweb /var/lib/localweb /var/log/localweb /var/run/localweb 2>/dev/null || true
chmod 750 /var/lib/localweb /var/log/localweb /var/run/localweb 2>/dev/null || true

chown root:localweb /etc/localweb
chmod 750 /etc/localweb

exit 0