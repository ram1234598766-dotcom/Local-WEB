#!/bin/bash
# LocalWEB post-install script for Debian/Ubuntu
# Runs after package installation

set -e

echo "LocalWEB post-install: configuring system..."

# Create localweb user/group if not exists
getent group localweb >/dev/null 2>&1 || groupadd --system localweb
getent passwd localweb >/dev/null 2>&1 || useradd --system --home-dir /var/lib/localweb --shell /usr/sbin/nologin --gid localweb localweb

# Create directories with correct permissions
mkdir -p /etc/localweb
mkdir -p /var/lib/localweb
mkdir -p /var/log/localweb
mkdir -p /var/run/localweb

chown -R localweb:localweb /var/lib/localweb /var/log/localweb /var/run/localweb
chmod 750 /var/lib/localweb /var/log/localweb /var/run/localweb

chown root:localweb /etc/localweb
chmod 750 /etc/localweb

# Create default config if not exists
if [ ! -f /etc/localweb/config.json ]; then
    cat > /etc/localweb/config.json << 'EOF'
{
  "node": {
    "name": "",
    "listen": "0.0.0.0:4443",
    "data_dir": "/var/lib/localweb",
    "storage": "/var/lib/localweb/data"
  },
  "transport": {
    "quic": {
      "max_idle_timeout": "30s",
      "keep_alive": "10s"
    },
    "hybrid_pq": false,
    "zero_rtt": true,
    "datagrams": true
  },
  "links": {
    "enabled": ["wifi", "wifi-direct", "ble", "usb", "acoustic"],
    "multipath": {
      "policy": "weighted_bw",
      "max_paths": 3
    }
  },
  "discovery": {
    "mdns": true,
    "ble": true,
    "rendezvous": {
      "url": "https://rendezvous.localweb.io",
      "register": true,
      "poll_interval": "60s"
    }
  },
  "dht": {
    "bootstrap": ["dht.localweb.io:4443"],
    "replication_factor": 10
  },
  "security": {
    "audit_log_max_size": "100MB",
    "pow_difficulty_target": "1s",
    "capability_ttl": "24h"
  },
  "qos": {
    "enabled": true,
    "default_class": "best_effort",
    "classes": 9
  },
  "gui": {
    "enabled": false,
    "listen": "localhost:8080",
    "theme": "system"
  },
  "plugins": {
    "enabled": true,
    "directory": "/var/lib/localweb/plugins",
    "allow_unsafe": false
  }
}
EOF
    chown root:localweb /etc/localweb/config.json
    chmod 640 /etc/localweb/config.json
fi

# Set capabilities on the binary for VPN support
if command -v setcap >/dev/null 2>&1; then
    setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb 2>/dev/null || true
    setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb-cli 2>/dev/null || true
    echo "Set capabilities on binaries"
fi

# Enable and start service
systemctl daemon-reload

# Enable service for auto-start
systemctl enable localweb 2>/dev/null || true

# Start service
if systemctl start localweb 2>/dev/null; then
    echo "LocalWEB service started successfully"
else
    echo "WARNING: Failed to start LocalWEB service. Check logs with: journalctl -u localweb -f"
fi

# Configure firewall if ufw is available
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
    ufw allow 4443/udp comment "LocalWEB QUIC" 2>/dev/null || true
    ufw allow 5353/udp comment "LocalWEB mDNS" 2>/dev/null || true
    ufw allow 8080/tcp comment "LocalWEB GUI" 2>/dev/null || true
    echo "Firewall rules added"
fi

# Configure firewall if firewalld is available
if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
    firewall-cmd --permanent --add-port=4443/udp 2>/dev/null || true
    firewall-cmd --permanent --add-port=5353/udp 2>/dev/null || true
    firewall-cmd --permanent --add-port=8080/tcp 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
    echo "Firewalld rules added"
fi

# Configure iptables directly if available
if command -v iptables >/dev/null 2>&1; then
    iptables -A INPUT -p udp --dport 4443 -j ACCEPT 2>/dev/null || true
    iptables -A INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
    iptables -A INPUT -p tcp --dport 8080 -j ACCEPT 2>/dev/null || true
    iptables-save > /etc/iptables/rules.v4 2>/dev/null || true
fi

# Create log directory
mkdir -p /var/log/localweb
chown -R localweb:localweb /var/log/localweb
chmod 750 /var/log/localweb

# Logrotate configuration
cat > /etc/logrotate.d/localweb << 'EOF'
/var/log/localweb/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 localweb localweb
    sharedscripts
    postrotate
        systemctl reload localweb > /dev/null 2>&1 || true
    endscript
}
EOF

echo "LocalWEB installation completed successfully"
echo ""
echo "Service status: systemctl status localweb"
echo "Logs: journalctl -u localweb -f"
echo "Config: /etc/localweb/config.json"
echo "Data: /var/lib/localweb"
echo ""
echo "To start the node manually: sudo systemctl start localweb"
echo "To enable auto-start: sudo systemctl enable localweb"
echo ""

exit 0