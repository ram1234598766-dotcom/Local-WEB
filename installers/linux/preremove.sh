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
# Ports this release opens, and ports older releases opened that it no longer
# does. The legacy list is removed too so that upgrading clears a stale rule
# rather than leaving 8080 open on a machine that no longer needs it.
#
# UDP: 4443 QUIC, 5353 DNS/mDNS discovery
# TCP: 8082 HTTP gateway, 587 SMTP, 993 IMAP, 9092 registry, 9094 DHT
LOCALWEB_UDP_PORTS="4443 5353"
LOCALWEB_TCP_PORTS="8082 587 993 9092 9094"
LOCALWEB_LEGACY_TCP_PORTS="8080"
    for port in $LOCALWEB_UDP_PORTS; do
        ufw delete allow "$port/udp" 2>/dev/null || true
    done
    for port in $LOCALWEB_TCP_PORTS $LOCALWEB_LEGACY_TCP_PORTS; do
        ufw delete allow "$port/tcp" 2>/dev/null || true
    done
fi

if command -v firewall-cmd >/dev/null 2>&1; then
    for port in $LOCALWEB_UDP_PORTS; do
        firewall-cmd --permanent --remove-port="$port/udp" 2>/dev/null || true
    done
    for port in $LOCALWEB_TCP_PORTS $LOCALWEB_LEGACY_TCP_PORTS; do
        firewall-cmd --permanent --remove-port="$port/tcp" 2>/dev/null || true
    done
    firewall-cmd --reload 2>/dev/null || true
fi

# Remove iptables rules
for port in ${LOCALWEB_UDP_PORTS}; do
    iptables -D INPUT -p udp --dport "$port" -j ACCEPT 2>/dev/null || true
done
for port in ${LOCALWEB_TCP_PORTS} ${LOCALWEB_LEGACY_TCP_PORTS}; do
    iptables -D INPUT -p tcp --dport "$port" -j ACCEPT 2>/dev/null || true
done
iptables-save > /etc/iptables/rules.v4 2>/dev/null || true

# Stop service if running
systemctl stop localweb 2>/dev/null || true
systemctl disable localweb 2>/dev/null || true

echo "Pre-remove completed"
exit 0