#!/bin/bash
# LocalWEB RPM pre-remove script

set -e

echo "LocalWEB RPM pre-remove: stopping services..."

# Stop and disable service
systemctl stop localweb 2>/dev/null || true
systemctl disable localweb 2>/dev/null || true

# Remove firewall rules
if command -v firewall-cmd >/dev/null 2>&1; then
    firewall-cmd --permanent --remove-port=4443/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=5353/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=8080/tcp 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
fi

# iptables
iptables -D INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
iptables-save > /etc/sysconfig/iptables 2>/dev/null || true

# Stop service
systemctl stop localweb 2>/dev/null || true
systemctl disable localweb 2>/dev/null || true

exit 0