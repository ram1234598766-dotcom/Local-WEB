#!/bin/bash
# LocalWEB post-remove script for Debian/Ubuntu
# Runs after package removal to clean up

set -e

echo "LocalWEB post-remove: cleaning up..."

# Remove systemd service files
rm -f /usr/lib/systemd/system/localweb.service
rm -f /usr/lib/systemd/system/localweb@.service
systemctl daemon-reload 2>/dev/null || true

# Remove user and group (if no other packages use them)
if ! getent passwd localweb >/dev/null 2>&1; then
    userdel localweb 2>/dev/null || true
fi

if ! getent group localweb >/dev/null 2>&1; then
    groupdel localweb 2>/dev/null || true
fi

# Remove logrotate config
rm -f /etc/logrotate.d/localweb

# Remove firewall rules (again, in case pre-remove didn't run)
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

# Remove data directory (commented out to preserve user data by default)
# Uncomment the following lines to remove user data on purge:
# if [ "$1" = "purge" ]; then
#     rm -rf /var/lib/localweb
#     rm -rf /var/log/localweb
#     rm -rf /var/run/localweb
#     rm -rf /etc/localweb
# fi

# Remove CLI symlink
rm -f /usr/local/bin/localweb-cli

echo "LocalWEB removed successfully"
exit 0