#!/bin/sh
# LocalWEB APK post-install

set -e

echo "LocalWEB APK post-install: configuring system..."

# Create config if not exists
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
    "quic": { "max_idle_timeout": "30s", "keep_alive": "10s" },
    "hybrid_pq": false, "zero_rtt": true, "datagrams": true
  },
  "links": { "enabled": ["wifi", "wifi-direct", "ble", "usb", "acoustic"], "multipath": { "policy": "weighted_bw", "max_paths": 3 } },
  "discovery": { "mdns": true, "ble": true, "rendezvous": { "url": "https://rendezvous.localweb.io", "register": true, "poll_interval": "60s" } },
  "dht": { "bootstrap": ["dht.localweb.io:4443"], "replication_factor": 10 },
  "security": { "audit_log_max_size": "100MB", "pow_difficulty_target": "1s", "capability_ttl": "24h" },
  "qos": { "enabled": true, "default_class": "best_effort", "classes": 9 },
  "gui": { "enabled": false, "listen": "localhost:8080", "theme": "system" },
  "plugins": { "enabled": true, "directory": "/var/lib/localweb/plugins", "allow_unsafe": false }
}
EOF
    chown root:localweb /etc/localweb/config.json
    chmod 640 /etc/localweb/config.json
fi

# Set capabilities
if command -v setcap >/dev/null 2>&1; then
    setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb 2>/dev/null || true
    setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb-cli 2>/dev/null || true
fi

# Enable service
rc-update add localweb default 2>/dev/null || true

# Start service
rc-service localweb start 2>/dev/null || true

# Logrotate
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
        rc-service localweb reload > /dev/null 2>&1 || true
    endscript
}
EOF

echo "LocalWEB installation completed"
exit 0