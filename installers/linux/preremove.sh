#!/bin/bash
# LocalWEB pre-remove script for Debian/Ubuntu
# Runs before package removal

set -e

echo "LocalWEB pre-remove: stopping services..."

# Stop and disable service
if systemctl is-active --quiet localweb 2>/dev/null; then
    echo "Stopping LocalWEB service..."
    systemctl stop localweb 2>/dev/null || true
fi

if systemctl is-enabled --quiet localweb 2>/dev/null; then
    echo "Disabling LocalWEB service..."
    systemctl disable localweb 2>/dev/null || true
fi

# Remove firewall rules
if command -v ufw >/dev/null 2>&1; then
    ufw delete allow 4443/udp 2>/dev/null || true
    ufw delete allow 5353/udp 2>/dev/null || true
    ufw delete allow 8080/tcp 2>/dev/null || true
fi

if command -v firewall-cmd >/dev/null 2>&1; then
    firewall-cmd --permanent --remove-port=4443/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=5353/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=8080/tcp 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
fi

# Remove iptables rules
iptables -D INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
iptables-save > /etc/iptables/rules.v4 2>/dev/null || true

# Stop service if running
systemctl stop localweb 2>/dev/null || true
systemctl disable localweb 2>/dev/null || true

echo "Pre-remove completed"
exit 0