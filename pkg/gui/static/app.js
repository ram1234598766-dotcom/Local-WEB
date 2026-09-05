// LocalWEB GUI — web SPA client
// Talks to real backend API endpoints (no mocked data)
// Connects to WebSocket for real-time peer updates

class LocalWEBApp {
  constructor() {
    this.state = {
      nodeID: '',
      peerCount: 0,
      peers: [],
      dhtNodes: [],
      auditLog: [],
      syncStatus: {},
      services: {},
      connected: false,
      loading: false,
    };
    this.ws = null;
    this.init();
  }

  init() {
    this.applySystemTheme();
    this.setupThemeToggle();
    this.setupRoutes();
    this.setupNav();
    this.connectWS();
    this.refresh();
  }

  applySystemTheme() {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const saved = localStorage.getItem('theme');
    if (saved === 'light' || (!saved && !prefersDark)) {
      document.documentElement.setAttribute('data-theme', 'light');
    } else {
      document.documentElement.setAttribute('data-theme', 'dark');
    }
  }

  setupThemeToggle() {
    const btn = document.getElementById('theme-toggle');
    btn.addEventListener('click', () => {
      const current = document.documentElement.getAttribute('data-theme');
      const next = current === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('theme', next);
      btn.querySelector('.theme-icon').textContent = next === 'dark' ? '◑' : '◐';
    });
  }

  setupRoutes() {
    this.routes = {
      dashboard: () => this.renderDashboard(),
      peers: () => this.renderPeers(),
      files: () => this.renderFiles(),
      dns: () => this.renderDNS(),
      http: () => this.renderHTTP(),
      email: () => this.renderEmail(),
      messaging: () => this.renderMessaging(),
      docs: () => this.renderDocs(),
      registry: () => this.renderRegistry(),
      voice: () => this.renderVoice(),
      vpn: () => this.renderVPN(),
      security: () => this.renderSecurity(),
      settings: () => this.renderSettings(),
    };
  }

  setupNav() {
    const items = [
      { id: 'dashboard', label: 'Dashboard', icon: '🏠' },
      { id: 'peers', label: 'Network', icon: '🌐' },
      { id: 'files', label: 'Files', icon: '📁' },
      { id: 'dns', label: 'DNS', icon: '🔍' },
      { id: 'http', label: 'HTTP', icon: '🌍' },
      { id: 'email', label: 'Email', icon: '✉️' },
      { id: 'messaging', label: 'Messaging', icon: '💬' },
      { id: 'docs', label: 'Docs', icon: '📝' },
      { id: 'registry', label: 'Registry', icon: '📦' },
      { id: 'voice', label: 'Voice', icon: '🎙️' },
      { id: 'vpn', label: 'VPN', icon: '🔒' },
      { id: 'security', label: 'Security', icon: '🛡️' },
      { id: 'settings', label: 'Settings', icon: '⚙' },
    ];

    const nav = document.getElementById('nav');
    nav.innerHTML = items.map(item =>
      `<div class="nav-item" data-route="${item.id}" tabindex="0">
        <span class="nav-icon">${item.icon}</span>${item.label}
      </div>`
    ).join('');

    document.querySelectorAll('.nav-item').forEach(el => {
      el.addEventListener('click', () => this.navigate(el.dataset.route));
      el.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          this.navigate(el.dataset.route);
        }
      });
    });

    const hash = window.location.hash.slice(1) || 'dashboard';
    this.navigate(hash);
  }

  navigate(route) {
    window.location.hash = '#' + route;
    document.querySelectorAll('.nav-item').forEach(el => {
      el.classList.toggle('active', el.dataset.route === route);
    });
    const renderer = this.routes[route];
    if (renderer) renderer();
  }

  async fetchAPI(path) {
    const resp = await fetch('/api' + path);
    if (!resp.ok) {
      throw new Error(`API ${path}: ${resp.status}`);
    }
    return resp.json();
  }

  async refresh() {
    try {
      const status = await this.fetchAPI('/status');
      this.state.nodeID = status.node_id;
      this.state.peerCount = status.peer_count;
      this.state.uptime = status.uptime;
      this.state.publicKey = status.public_key;
    } catch (e) {
      this.showToast('Failed to load node status', 'error');
    }
  }

  connectWS() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/api/events`;
    this.ws = new WebSocket(wsUrl);

    this.ws.addEventListener('open', () => {
      this.state.connected = true;
      document.title = 'LocalWEB';
    });

    this.ws.addEventListener('message', (event) => {
      const data = JSON.parse(event.data.split('\n').pop());
      this.dispatchEvent(data);
    });

    this.ws.addEventListener('close', () => {
      this.state.connected = false;
      setTimeout(() => this.connectWS(), 2000);
    });

    this.ws.addEventListener('error', (err) => {
      console.error('WebSocket error:', err);
    });
  }

  dispatchEvent(data) {
    if (data.type === 'peer_connected' || data.type === 'peer_disconnected') {
      this.refreshPeers();
    }
  }

  async refreshPeers() {
    try {
      this.state.peers = await this.fetchAPI('/peers');
      this.state.peerCount = this.state.peers.length;
      if (document.querySelector('.nav-item.active')?.dataset.route === 'peers') {
        this.renderPeers();
      }
    } catch (e) {
      // silent — peers may not be available yet
    }
  }

  showToast(message, type = 'info') {
    const container = document.getElementById('notifications');
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    toast.setAttribute('role', 'alert');
    container.appendChild(toast);
    setTimeout(() => container.removeChild(toast), 5000);
  }

  renderDashboard() {
    this.showLoading(true);
    Promise.all([
      this.fetchAPI('/status'),
      this.fetchAPI('/peers'),
      this.fetchAPI('/services/health'),
    ]).then(([status, peers, services]) => {
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="grid grid-cols-3">
          <div class="stat-card">
            <div class="stat-label">Node ID</div>
            <div class="stat-value" style="font-size: 1rem; word-break: break-all;">${status.node_id}</div>
          </div>
          <div class="stat-card">
            <div class="stat-label">Uptime</div>
            <div class="stat-value">${status.uptime}</div>
          </div>
          <div class="stat-card">
            <div class="stat-label">Connected Peers</div>
            <div class="stat-value success">${peers.length}</div>
          </div>
        </div>
        <div style="margin-top: 1.5rem;">
          <div class="card">
            <div class="card-header">Services</div>
            <div class="card-body">
              <table class="table">
                <thead><tr><th>Service</th><th>Status</th></tr></thead>
                <tbody>
                  ${Object.entries(services.services).map(([name, healthy]) => `
                    <tr>
                      <td>${name}</td>
                      <td><span style="color: ${healthy ? 'var(--color-success)' : 'var(--color-critical)'}">
                        ${healthy ? 'online' : 'offline'}
                      </span></td>
                    </tr>
                  `).join('')}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      `;
    }).catch(() => {
      this.showLoading(false);
    });
  }

  async renderPeers() {
    this.showLoading(true);
    try {
      const [peers, dht] = await Promise.all([
        this.fetchAPI('/peers'),
        this.fetchAPI('/dht/table').catch(() => ({ nodes: [] })),
      ]);
      this.showLoading(false);

      const peerNodes = peers.length > 0 ? peers : dht.nodes;
      this.state.peers = peers;
      this.state.peerCount = peers.length;

      const topologySVG = this.renderTopology(peers, dht.nodes);

      document.getElementById('content').innerHTML = `
        <div class="grid grid-cols-2">
          <div class="card">
            <div class="card-header">Topology</div>
            <div class="card-body">
              ${topologySVG}
            </div>
          </div>
          <div class="card">
            <div class="card-header">Connected Peers (${peers.length})</div>
            <div class="card-body">
              ${peers.length === 0 ?
                '<p style="color: var(--color-text-muted);">No peers connected. Check network settings.</p>' :
                '<div class="list">' + peers.map(p => `
                  <div class="connection">
                    <span class="connection-status online"></span>
                    <div class="connection-info">
                      <div class="connection-name">${p.name || p.id}</div>
                      <div class="connection-meta">${p.source} • ${p.latency || 'n/a'}</div>
                    </div>
                  </div>
                `).join('') + '</div>'
              }
            </div>
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
      this.showToast('Failed to load peers', 'error');
    }
  }

  renderTopology(peers, dhtNodes) {
    const allNodes = dhtNodes.length > 0 ? dhtNodes : peers;
    if (allNodes.length === 0) {
      return `<div class="topology" style="display: flex; align-items: center; justify-content: center;">
        <p style="color: var(--color-text-muted);">No nodes in topology</p>
      </div>`;
    }

    const width = 400;
    const height = 300;
    const cx = width / 2;
    const cy = height / 2;
    const count = allNodes.length;

    let svg = `<div class="topology"><svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">`;

    // Draw connections (lines between nodes)
    for (let i = 0; i < count; i++) {
      const a = allNodes[i];
      for (let j = i + 1; j < count; j++) {
        const b = allNodes[j];
        const ax = cx + Math.cos(2 * Math.PI * i / count) * 100;
        const ay = cy + Math.sin(2 * Math.PI * i / count) * 100;
        const bx = cx + Math.cos(2 * Math.PI * j / count) * 100;
        const by = cy + Math.sin(2 * Math.PI * j / count) * 100;
        svg += `<line x1="${ax}" y1="${ay}" x2="${bx}" y2="${by}" stroke="var(--color-border)" stroke-width="1" />`;
      }
    }

    // Draw node dots + labels
    for (let i = 0; i < count; i++) {
      const n = allNodes[i];
      const angle = 2 * Math.PI * i / count;
      const x = cx + Math.cos(angle) * 100;
      const y = cy + Math.sin(angle) * 100;
      const shortID = (n.id || n.ID || '').substring(0, 8);
      const isSelf = i === 0;
      const dotColor = isSelf ? 'var(--color-primary)' : 'var(--color-accent)';

      svg += `<circle cx="${x}" cy="${y}" r="8" fill="${dotColor}" stroke="var(--color-bg)" stroke-width="2" />`;
      svg += `<text x="${x}" y="${y + 30}" text-anchor="middle" font-size="8" fill="var(--color-text-muted)">${shortID}</text>`;
    }

    // Center node (this node)
    const centerLabel = 'you';
    svg += `<circle cx="${cx}" cy="${cy}" r="10" fill="var(--color-primary)" stroke="var(--color-bg)" stroke-width="2" />`;
    svg += `<text x="${cx}" y="${cy + 35}" text-anchor="middle" font-size="8" fill="var(--color-text-muted)">${centerLabel}</text>`;

    // Draw connection from center to each node
    for (let i = 0; i < count; i++) {
      const angle = 2 * Math.PI * i / count;
      const x = cx + Math.cos(angle) * 100;
      const y = cy + Math.sin(angle) * 100;
      svg += `<line x1="${cx}" y1="${cy}" x2="${x}" y2="${y}" stroke="var(--color-primary)" stroke-width="1.5" opacity="0.6" />`;
    }

    svg += `</svg></div>`;
    return svg;
  }

  async renderFiles() {
    this.showLoading(true);
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Files</div><div class="card-body"><p style="color: var(--color-text-muted);">File sharing via DHT block exchange. Drop files in the node directory to share.</p></div></div>';
    this.showLoading(false);
  }

  renderDNS() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">DNS</div><div class="card-body"><p style="color: var(--color-text-muted);">Browse .localweb records.</p></div></div>';
  }

  renderHTTP() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">HTTP Gateway</div><div class="card-body"><p style="color: var(--color-text-muted);">Per-site routing configuration.</p></div></div>';
  }

  renderEmail() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Email</div><div class="card-body"><p style="color: var(--color-text-muted);">SMTP + IMAP with PoW antispam.</p></div></div>';
  }

  renderMessaging() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Messaging</div><div class="card-body"><p style="color: var(--color-text-muted);">Signed pub/sub channels.</p></div></div>';
  }

  renderDocs() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Docs</div><div class="card-body"><p style="color: var(--color-text-muted);">RGA-backed collaborative text editor.</p></div></div>';
  }

  renderRegistry() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Registry</div><div class="card-body"><p style="color: var(--color-text-muted);">Browse packages via DHT.</p></div></div>';
  }

  renderVoice() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">Voice</div><div class="card-body"><p style="color: var(--color-text-muted);">ICE + Opus/VP9 calls.</p></div></div>';
  }

  renderVPN() {
    document.getElementById('content').innerHTML = '<div class="card"><div class="card-header">VPN</div><div class="card-body"><p style="color: var(--color-text-muted);">TUN tunnel over Noise-secured QUIC.</p></div></div>';
  }

  async renderSecurity() {
    this.showLoading(true);
    try {
      const [auditLog, verify, status] = await Promise.all([
        this.fetchAPI('/audit-log'),
        this.fetchAPI('/audit-log/verify'),
        this.refresh(),
      ]);
      this.showLoading(false);

      const integrityColor = verify.verified
        ? 'var(--color-success)'
        : 'var(--color-critical)';
      const integrityText = verify.verified
        ? 'Chain verified'
        : 'TAMPER DETECTED';

      document.getElementById('content').innerHTML = `
        <div class="grid grid-cols-2">
          <div class="card">
            <div class="card-header">Node Identity</div>
            <div class="card-body">
              <div class="form-group">
                <label class="form-label">Node ID</label>
                <div style="word-break: break-all; font-size: 0.8125rem; padding: 0.5rem; background: var(--color-bg); border-radius: 0.25rem;">${status.nodeID}</div>
              </div>
              <div class="form-group">
                <label class="form-label">Public Key</label>
                <div style="word-break: break-all; font-size: 0.8125rem; padding: 0.5rem; background: var(--color-bg); border-radius: 0.25rem;">${status.publicKey}</div>
              </div>
              <button class="btn btn-primary btn-sm" style="margin-top: 0.5rem;" onclick="app.copyPublicKey()">
                Copy Public Key
              </button>
              <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-top: 0.5rem;">Keep this key safe. It identifies this node on the network.</p>
            </div>
          </div>
          <div class="card">
            <div class="card-header">
              Audit Log Integrity
              <span style="color: ${integrityColor}; font-weight: 600; font-size: 0.75rem;">● ${integrityText}</span>
            </div>
            <div class="card-body">
              <p style="color: var(--color-text-muted); font-size: 0.75rem; margin-bottom: 0.5rem;">
                Last verified: ${new Date(verify.timestamp).toLocaleTimeString()}
              </p>
              <table class="table">
                <thead><tr><th>Type</th><th>Peer</th><th>Time</th></tr></thead>
                <tbody>
                  ${auditLog.slice(0, 20).map(e => `
                    <tr>
                      <td>${e.type}</td>
                      <td>${e.peer_id || 'system'}</td>
                      <td>${new Date(e.timestamp).toLocaleTimeString()}</td>
                    </tr>
                  `).join('')}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
      this.showToast('Failed to load security info', 'error');
    }
  }

  copyPublicKey() {
    const keyEl = document.querySelector('.card-body div[style*="word-break"]');
    if (keyEl) {
      const keyText = keyEl.textContent;
      navigator.clipboard.writeText(keyText);
      this.showToast('Public key copied to clipboard', 'success');
    }
  }

  renderSettings() {
    document.getElementById('content').innerHTML = `
      <div class="card">
        <div class="card-header">Settings</div>
        <div class="card-body">
          <label class="toggle">
            <input type="checkbox" id="advanced-toggle" style="margin-right: 0.5rem;" />
            <span>Show advanced options</span>
          </label>
          <div id="advanced-options" style="display: none; margin-top: 1rem;">
            <div class="form-group">
              <label class="form-label">Listen Address</label>
              <input type="text" class="form-input" value="0.0.0.0:4443" readonly />
            </div>
            <div class="form-group">
              <label class="form-label">HTTP Gateway</label>
              <input type="text" class="form-input" value="localhost:8080" readonly />
            </div>
          </div>
        </div>
      </div>
    `;
    const toggle = document.getElementById('advanced-toggle');
    const opts = document.getElementById('advanced-options');
    toggle.addEventListener('change', () => {
      opts.style.display = toggle.checked ? 'block' : 'none';
    });
  }

  showLoading(show) {
    this.state.loading = show;
    const content = document.getElementById('content');
    if (show) {
      content.innerHTML = '<div style="text-align: center; padding: 2rem; color: var(--color-text-muted);">Loading…</div>';
    }
  }
}

document.addEventListener('DOMContentLoaded', () => {
  new LocalWEBApp();
});

window.addEventListener('hashchange', () => {
  const route = window.location.hash.slice(1) || 'dashboard';
  window.app?.navigate(route);
});
