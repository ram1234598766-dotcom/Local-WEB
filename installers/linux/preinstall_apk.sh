#!/bin/sh
# LocalWEB APK pre-install

set -e

echo "LocalWEB APK pre-install: checking prerequisites..."

# Check for systemd
if ! command -v systemctl >/dev/null 2>&1; then
    echo "ERROR: systemd is required but not found"
    exit 1
fi

# Create localweb user/group
addgroup -S localweb 2>/dev/null || true
adduser -S -D -H -h /var/lib/localweb -s /sbin/nologin -G localweb localweb 2>/dev/null || true

# Stop existing service
rc-service localweb stop 2>/dev/null || true
rc-update del localweb 2>/dev/null || true

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