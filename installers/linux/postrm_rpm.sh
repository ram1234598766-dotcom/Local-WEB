#!/bin/bash
# LocalWEB RPM post-remove script

set -e

echo "LocalWEB RPM post-remove: cleaning up..."

# Remove systemd service files
rm -f /usr/lib/systemd/system/localweb.service
rm -f /usr/lib/systemd/system/localweb@.service
systemctl daemon-reload 2>/dev/null || true

# Remove user/group
userdel localweb 2>/dev/null || true
groupdel localweb 2>/dev/null || true

# Remove logrotate
rm -f /etc/logrotate.d/localweb

# Remove firewall rules (again)
if command -v firewall-cmd >/dev/null 2>&1; then
    firewall-cmd --permanent --remove-port=4443/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=5353/udp 2>/dev/null || true
    firewall-cmd --permanent --remove-port=8080/tcp 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
fi

iptables -D INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
iptables -D INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
iptables-save > /etc/sysconfig/iptables 2>/dev/null || true

# Remove data directory (only on purge, not upgrade)
# rpm passes $1 as 0 for erase, 1 for upgrade
if [ "$1" = "0" ]; then
    # This is a purge/erase, not upgrade
    rm -rf /var/lib/localweb
    rm -rf /var/log/localweb
    rm -rf /var/run/localweb
    rm -rf /etc/localweb
fi

# Remove CLI symlink
rm -f /usr/local/bin/localweb-cli

echo "LocalWEB removed successfully"
exit 0