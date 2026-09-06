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