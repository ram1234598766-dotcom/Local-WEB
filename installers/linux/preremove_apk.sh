#!/bin/sh
# LocalWEB APK pre-remove

set -e

echo "LocalWEB APK pre-remove: stopping services..."

# Stop service
rc-service localweb stop 2>/dev/null || true
rc-update del localweb 2>/dev/null || true

# Firewall cleanup
if command -v iptables >/dev/null 2>&1; then
    iptables -D INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
    iptables -D INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
    iptables -D INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
fi

exit 0