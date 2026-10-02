#!/bin/sh
# LocalWEB APK post-remove

set -e

echo "LocalWEB APK post-remove: cleaning up..."

# Remove service
rc-update del localweb 2>/dev/null || true

# Remove logrotate
rm -f /etc/logrotate.d/localweb

# Remove user/group
deluser localweb 2>/dev/null || true
delgroup localweb 2>/dev/null || true

# Firewall cleanup
if command -v iptables >/dev/null 2>&1; then
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
        iptables -D INPUT -p udp --dport "$port" -j ACCEPT 2>/dev/null || true
    done
    for port in $LOCALWEB_TCP_PORTS $LOCALWEB_LEGACY_TCP_PORTS; do
        iptables -D INPUT -p tcp --dport "$port" -j ACCEPT 2>/dev/null || true
    done
fi

# Remove data (only on purge)
# In APK, $1 contains "purge" when purging
if [ "$1" = "purge" ]; then
    rm -rf /var/lib/localweb
    rm -rf /var/log/localweb
    rm -rf /var/run/localweb
    rm -rf /etc/localweb
fi

# Remove CLI symlink
rm -f /usr/local/bin/localweb-cli

echo "LocalWEB removed successfully"
exit 0