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
    iptables -D INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
    iptables -D INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
    iptables -D INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
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