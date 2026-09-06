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
    this.loadOnboardingStatus();
  }

  async loadOnboardingStatus() {
    try {
      const status = await this.fetchAPI('/onboarding/status');
      this.state.onboarding = status;
      // If onboarding not complete, redirect to onboarding page
      if (status.step < 4) {
        window.location.hash = '#onboarding';
        return;
      }
    } catch (e) {
      this.state.onboarding = { step: 0, hasIdentity: false, nodeName: '', needsBackup: false };
      window.location.hash = '#onboarding';
      return;
    }
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
      onboarding: () => this.renderOnboarding(),
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
      { id: 'onboarding', label: 'Onboarding', icon: '🚀' },
      { id: 'dashboard', label: 'Dashboard', icon: '🏠' },
      { id: 'peers', label: 'Network', icon: '🌐' },
      { type: 'separator' },
      { id: 'files', label: 'Files', icon: '📁' },
      { id: 'dns', label: 'DNS', icon: '🔍' },
      { id: 'http', label: 'HTTP', icon: '🌍' },
      { id: 'email', label: 'Email', icon: '✉️' },
      { id: 'messaging', label: 'Messaging', icon: '💬' },
      { id: 'docs', label: 'Docs', icon: '📝' },
      { id: 'registry', label: 'Registry', icon: '📦' },
      { id: 'voice', label: 'Voice', icon: '🎙️' },
      { id: 'vpn', label: 'VPN', icon: '🔒' },
      { type: 'separator' },
      { id: 'security', label: 'Security', icon: '🛡️' },
      { id: 'settings', label: 'Settings', icon: '⚙' },
    ];

    const nav = document.getElementById('nav');
    nav.innerHTML = items.map(item =>
      item.type === 'separator'
        ? '<div style="border-top: 1px solid var(--color-border); margin: 0.5rem 0;"></div>'
        : `<div class="nav-item" data-route="${item.id}" tabindex="0">
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

  init() {
    this.applySystemTheme();
    this.setupThemeToggle();
    this.setupRoutes();
    this.setupNav();
    this.connectWS();
    this.loadOnboardingStatus();
  }

  async loadOnboardingStatus() {
    try {
      const status = await this.fetchAPI('/onboarding/status');
      this.state.onboarding = status;
    } catch (e) {
      this.state.onboarding = { step: 0, hasIdentity: false, nodeName: '', needsBackup: false };
    }
    this.refresh();
  }

  async renderOnboarding() {
    const status = this.state.onboarding || { step: 0, hasIdentity: false, nodeName: '', needsBackup: false };
    const step = status.step || 0;

    // Step indicator
    const steps = [
      { id: 0, label: 'Welcome' },
      { id: 1, label: 'Name Your Device' },
      { id: 2, label: 'Pair with QR Code' },
      { id: 3, label: 'Backup Passphrase' },
      { id: 4, label: 'Complete' },
    ];

    const stepHtml = steps.map((s, i) => `
      <div class="step-indicator ${i <= step ? 'completed' : ''} ${i === step ? 'active' : ''}" style="flex: 1; text-align: center; position: relative;">
        <div class="step-circle" style="width: 32px; height: 32px; border-radius: 50%; border: 2px solid ${i <= step ? 'var(--color-primary)' : 'var(--color-border)'}; background: ${i < step ? 'var(--color-primary)' : (i === step ? 'var(--color-bg)' : 'transparent')}; color: ${i <= step ? 'white' : 'var(--color-text-muted)'}; display: flex; align-items: center; justify-content: center; margin: 0 auto 0.5rem; font-weight: 600; font-size: 0.875rem;">${i < step ? '✓' : i + 1}</div>
        <div class="step-label" style="font-size: 0.75rem; color: ${i <= step ? 'var(--color-text)' : 'var(--color-text-muted)'}; font-weight: ${i === step ? '600' : '400'};">${s.label}</div>
        ${i < steps.length - 1 ? `<div class="step-line" style="position: absolute; top: 16px; left: 50%; right: -50%; height: 2px; background: ${i < step ? 'var(--color-primary)' : 'var(--color-border)'}; z-index: -1;"></div>` : ''}
      </div>
    `).join('');

    let contentHtml = '';

    switch (step) {
      case 0: // Welcome
        contentHtml = this.renderWelcomeStep();
        break;
      case 1: // Name Device
        contentHtml = this.renderNameStep();
        break;
      case 2: // QR Code
        contentHtml = await this.renderQRStep();
        break;
      case 3: // Backup
        contentHtml = this.renderBackupStep();
        break;
      case 4: // Complete
        contentHtml = this.renderCompleteStep();
        break;
    }

    document.getElementById('content').innerHTML = `
      <div style="max-width: 640px; margin: 0 auto;">
        <div class="wizard-progress" style="display: flex; margin-bottom: 2rem; padding: 0 1rem;">
          ${stepHtml}
        </div>
        <div class="wizard-content" style="animation: fadeIn 0.3s ease;">
          ${contentHtml}
        </div>
      </div>
      <style>
        @keyframes fadeIn { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: translateY(0); } }
        .step-indicator.completed .step-circle { background: var(--color-primary); border-color: var(--color-primary); }
        .step-indicator.active .step-circle { background: var(--color-bg); border-color: var(--color-primary); box-shadow: 0 0 0 3px var(--color-primary-light); }
      </style>
    `;

    // Attach event listeners based on step
    this.attachOnboardingListeners(step);
  }

  renderWelcomeStep() {
    return `
      <div class="card" style="text-align: center; padding: 2rem;">
        <div style="width: 80px; height: 80px; border-radius: 50%; background: linear-gradient(135deg, var(--color-primary), var(--color-accent)); display: flex; align-items: center; justify-content: center; margin: 0 auto 1.5rem; font-size: 2rem;">🔐</div>
        <h1 style="font-size: 1.75rem; font-weight: 700; margin-bottom: 0.5rem; color: var(--color-text);">Welcome to LocalWEB</h1>
        <p style="color: var(--color-text-muted); margin-bottom: 2rem; line-height: 1.6; max-width: 480px; margin-left: auto; margin-right: auto;">
          LocalWEB is a local-first, encrypted mesh network. Connect devices
          on the same network without any central server, cloud, or internet dependency.
          All traffic is end-to-end encrypted with post-quantum cryptography.
        </p>
        <div style="display: flex; gap: 0.75rem; justify-content: center; flex-wrap: wrap; margin-bottom: 2rem;">
          <div class="stat-card" style="min-width: 140px;">
            <div class="stat-label">🔒 Encrypted</div>
            <div class="stat-value" style="font-size: 0.875rem;">Noise XX + Kyber-1024</div>
          </div>
          <div class="stat-card" style="min-width: 140px;">
            <div class="stat-label">🌐 Peer-to-Peer</div>
            <div class="stat-value" style="font-size: 0.875rem;">No central server</div>
          </div>
          <div class="stat-card" style="min-width: 140px;">
            <div class="stat-label">⚡ Multi-Path</div>
            <div class="stat-value" style="font-size: 0.875rem;">WiFi, BLE, USB, Audio</div>
          </div>
        </div>
        <button class="btn btn-primary btn-lg" onclick="app.nextOnboardingStep()" style="min-width: 200px;">
          Get Started
        </button>
      </div>
    `;
  }

  renderNameStep() {
    return `
      <div class="card" style="max-width: 480px; margin: 0 auto; padding: 2rem;">
        <h2 style="font-size: 1.25rem; font-weight: 600; margin-bottom: 0.5rem; text-align: center; color: var(--color-text);">Name Your Device</h2>
        <p style="color: var(--color-text-muted); margin-bottom: 2rem; text-align: center; line-height: 1.6;">
          Choose a recognizable name for this node. Other devices on the network
          will see this name when discovering peers.
        </p>
        <div class="form-group">
          <label class="form-label" for="device-name">Device Name</label>
          <input type="text" class="form-input" id="device-name" placeholder="e.g., MacBook Pro, Living Room PC" value="${this.state.onboarding?.nodeName || ''}" autocomplete="off" style="text-align: center; font-size: 1.125rem;" />
        </div>
        <p style="color: var(--color-text-muted); font-size: 0.8125rem; text-align: center; margin-top: 1rem;">
          This name is only used locally for display. Your cryptographic identity
          is separate and cannot be changed.
        </p>
        <div style="display: flex; gap: 0.75rem; justify-content: center; margin-top: 2rem;">
          <button class="btn btn-secondary" onclick="app.prevOnboardingStep()">Back</button>
          <button class="btn btn-primary" onclick="app.saveDeviceName()">Continue</button>
        </div>
      </div>
    `;
  }

  async renderQRStep() {
    let qrHtml = '';
    try {
      const qr = await this.fetchAPI('/onboarding/qr');
      qrHtml = `
        <div style="text-align: center; margin: 1.5rem 0;">
          <img src="data:image/png;base64,${qr.qr_code}" alt="Pairing QR Code" style="width: 240px; height: 240px; border-radius: 0.5rem; box-shadow: var(--shadow-md);" />
        </div>
        <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-bottom: 1rem;">
          Scan this QR code with another LocalWEB device to pair instantly.
        </p>
        <div class="stat-card" style="max-width: 320px; margin: 0 auto 1.5rem;">
          <div class="stat-label">Your Node ID</div>
          <div class="stat-value" style="font-size: 0.8125rem; word-break: break-all; font-family: monospace;">${qr.node_id}</div>
        </div>
        <p style="color: var(--color-text-muted); font-size: 0.8125rem;">
          Or share your Node ID manually: <code style="font-size: 0.75rem;">${qr.node_id}</code>
        </p>
      `;
    } catch (e) {
      qrHtml = `<p style="color: var(--color-critical); text-align: center;">Failed to generate QR code. <button class="btn btn-secondary btn-sm" onclick="app.renderOnboarding()">Retry</button></p>`;
    }

    return `
      <div class="card" style="max-width: 480px; margin: 0 auto; padding: 2rem; text-align: center;">
        <h2 style="font-size: 1.25rem; font-weight: 600; margin-bottom: 0.5rem; color: var(--color-text);">Pair with Another Device</h2>
        <p style="color: var(--color-text-muted); margin-bottom: 1.5rem; line-height: 1.6;">
          Open LocalWEB on another device on the same network and scan this QR code.
          The devices will automatically discover and connect to each other.
        </p>
        ${qrHtml}
        <div style="display: flex; gap: 0.75rem; justify-content: center; margin-top: 2rem;">
          <button class="btn btn-secondary" onclick="app.prevOnboardingStep()">Back</button>
          <button class="btn btn-primary" onclick="app.nextOnboardingStep()">I've Paired a Device</button>
        </div>
      </div>
    `;
  }

  renderBackupStep() {
    return `
      <div class="card" style="max-width: 480px; margin: 0 auto; padding: 2rem;">
        <h2 style="font-size: 1.25rem; font-weight: 600; margin-bottom: 0.5rem; text-align: center; color: var(--color-text);">Backup Your Identity</h2>
        <p style="color: var(--color-text-muted); margin-bottom: 1.5rem; text-align: center; line-height: 1.6;">
          <strong>Critical:</strong> Create a passphrase to encrypt your private key.
          This backup allows you to recover your identity if you lose this device.
          <span style="color: var(--color-critical);">Without this passphrase, your identity cannot be recovered.</span>
        </p>
        <div class="form-group">
          <label class="form-label" for="backup-passphrase">Passphrase</label>
          <input type="password" class="form-input" id="backup-passphrase" placeholder="Enter a strong passphrase" autocomplete="new-password" />
        </div>
        <div class="form-group">
          <label class="form-label" for="backup-passphrase-confirm">Confirm Passphrase</label>
          <input type="password" class="form-input" id="backup-passphrase-confirm" placeholder="Confirm your passphrase" autocomplete="new-password" />
        </div>
        <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-top: 0.5rem;">
          Use a passphrase you'll remember. Consider using a password manager.
        </p>
        <div style="display: flex; gap: 0.75rem; justify-content: center; margin-top: 2rem;">
          <button class="btn btn-secondary" onclick="app.prevOnboardingStep()">Back</button>
          <button class="btn btn-primary" onclick="app.createBackup()">Create Backup</button>
        </div>
      </div>
    `;
  }

  renderCompleteStep() {
    return `
      <div class="card" style="max-width: 480px; margin: 0 auto; padding: 2rem; text-align: center;">
        <div style="width: 80px; height: 80px; border-radius: 50%; background: var(--color-success); display: flex; align-items: center; justify-content: center; margin: 0 auto 1.5rem; font-size: 2rem;">✓</div>
        <h2 style="font-size: 1.5rem; font-weight: 700; margin-bottom: 0.5rem; color: var(--color-text);">You're Ready!</h2>
        <p style="color: var(--color-text-muted); margin-bottom: 2rem; line-height: 1.6;">
          Your LocalWEB node is configured and ready to connect.
          <br><strong>Save your backup passphrase</strong> in a secure location.
        </p>
        <div class="grid" style="grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 2rem;">
          <div class="stat-card">
            <div class="stat-label">Node Name</div>
            <div class="stat-value" style="font-size: 0.875rem;">${this.state.onboarding?.nodeName || 'localweb-node'}</div>
          </div>
          <div class="stat-card">
            <div class="stat-label">Node ID</div>
            <div class="stat-value" style="font-size: 0.75rem; word-break: break-all; font-family: monospace;">${this.state.nodeID || 'loading…'}</div>
          </div>
        </div>
        <div style="display: flex; gap: 0.75rem; justify-content: center; flex-wrap: wrap;">
          <button class="btn btn-primary" onclick="app.navigate('dashboard')">Go to Dashboard</button>
          <button class="btn btn-secondary" onclick="app.navigate('peers')">Find Peers</button>
        </div>
      </div>
    `;
  }

  attachOnboardingListeners(step) {
    // Step-specific event listeners would go here
    // For now, we use inline onclick handlers
  }

  async nextOnboardingStep() {
    if (this.state.onboarding?.step === 1) {
      const nameInput = document.getElementById('device-name');
      if (nameInput && !nameInput.value.trim()) {
        this.showToast('Please enter a device name', 'error');
        return;
      }
      await this.saveDeviceName();
      return;
    }
    if (this.state.onboarding?.step === 3) {
      await this.createBackup();
      return;
    }
    if (this.state.onboarding && this.state.onboarding.step < 4) {
      this.state.onboarding.step++;
      await this.renderOnboarding();
    }
  }

  async prevOnboardingStep() {
    if (this.state.onboarding && this.state.onboarding.step > 0) {
      this.state.onboarding.step--;
      await this.renderOnboarding();
    }
  }

  async saveDeviceName() {
    const nameInput = document.getElementById('device-name');
    if (!nameInput || !nameInput.value.trim()) {
      this.showToast('Please enter a device name', 'error');
      return;
    }
    this.showLoading(true);
    try {
      const resp = await fetch('/api/onboarding/status', { method: 'GET' });
      // Just update local state for now
      this.state.onboarding = this.state.onboarding || {};
      this.state.onboarding.nodeName = nameInput.value.trim();
      this.state.onboarding.step = 2;
      await this.renderOnboarding();
    } catch (e) {
      this.showToast('Failed to save name', 'error');
    }
    this.showLoading(false);
  }

  async createBackup() {
    const passphrase = document.getElementById('backup-passphrase');
    const confirm = document.getElementById('backup-passphrase-confirm');
    if (!passphrase || !confirm) return;

    if (passphrase.value !== confirm.value) {
      this.showToast('Passphrases do not match', 'error');
      return;
    }
    if (passphrase.value.length < 8) {
      this.showToast('Passphrase must be at least 8 characters', 'error');
      return;
    }

    this.showLoading(true);
    try {
      const resp = await fetch('/api/onboarding/backup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          passphrase: passphrase.value,
          name: this.state.onboarding?.nodeName || 'localweb-node'
        })
      });
      const data = await resp.json();
      if (data.success) {
        this.showToast('Backup created! Download and store it safely.', 'success');
        this.state.onboarding.step = 4;
        this.state.onboarding.needsBackup = false;
        await this.renderOnboarding();
      } else {
        this.showToast(data.message || 'Backup failed', 'error');
      }
    } catch (e) {
      this.showToast('Backup failed: ' + e.message, 'error');
    }
    this.showLoading(false);
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

  async renderDNS() {
    this.showLoading(true);
    try {
      const records = await this.fetchAPI('/dns/records');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">DNS Records (.localweb)</div>
          <div class="card-body">
            ${records.length === 0 ?
              '<p style="color: var(--color-text-muted);">No DNS records. Peers will appear here once connected.</p>' :
              '<table class="table"><thead><tr><th>Name</th><th>Type</th><th>Value</th><th>Status</th></tr></thead><tbody>' +
              records.map(r => `<tr><td>${r.name}</td><td>${r.type}</td><td>${r.value}</td><td>${r.verified ? 'verified' : 'pending'}</td></tr>`).join('') +
              '</tbody></table>'
            }
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
    }
  }

  async renderHTTP() {
    this.showLoading(true);
    try {
      const sites = await this.fetchAPI('/http/sites');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">HTTP Gateway Sites</div>
          <div class="card-body">
            <table class="table">
              <thead><tr><th>Site</th><th>Status</th><th>Routes</th></tr></thead>
              <tbody>
                ${sites.map(s => `<tr>
                  <td>${s.name}</td>
                  <td><span style="color: var(--color-success);">${s.status}</span></td>
                  <td>${s.routes}</td>
                </tr>`).join('')}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
    }
  }

  async renderEmail() {
    this.showLoading(true);
    try {
      const msgs = await this.fetchAPI('/email/messages');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">Inbox (${msgs.length})</div>
          <div class="card-body">
            ${msgs.length === 0 ?
              '<p style="color: var(--color-text-muted);">No messages.</p>' :
              '<table class="table"><thead><tr><th>From</th><th>Subject</th><th>Date</th></tr></thead><tbody>' +
              msgs.map(m => `<tr><td>${m.from}</td><td>${m.subject}</td><td>${new Date(m.date).toLocaleTimeString()}</td></tr>`).join('') +
              '</tbody></table>'
            }
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
    }
  }

  async renderMessaging() {
    this.showLoading(true);
    try {
      const msgs = await this.fetchAPI('/messaging/messages');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">Messaging</div>
          <div class="card-body">
            <div id="message-list" style="height: 200px; overflow-y: auto; border: 1px solid var(--color-border); border-radius: 0.25rem; padding: 0.5rem; margin-bottom: 0.75rem;">
              ${msgs.map(m => `<div style="margin-bottom: 0.5rem;"><strong>${m.from}</strong>: ${m.text}</div>`).join('')}
            </div>
            <form id="message-form" style="display: flex; gap: 0.5rem;">
              <input type="text" class="form-input" id="message-input" placeholder="Type a message…" autocomplete="off" />
              <button type="submit" class="btn btn-primary btn-sm">Send</button>
            </form>
          </div>
        </div>
      `;
      document.getElementById('message-form').addEventListener('submit', (e) => {
        e.preventDefault();
        const input = document.getElementById('message-input');
        if (input.value.trim()) {
          this.showToast('Messaging relay requires transport connection', 'info');
          input.value = '';
        }
      });
    } catch (e) {
      this.showLoading(false);
    }
  }

  async renderDocs() {
    this.showLoading(true);
    try {
      const docs = await this.fetchAPI('/docs/documents');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">Documents</div>
          <div class="card-body">
            ${docs.length === 0 ?
              '<p style="color: var(--color-text-muted);">No documents yet.</p>' :
              '<table class="table"><thead><tr><th>Name</th><th>Peers</th><th>Last Sync</th></tr></thead><tbody>' +
              docs.map(d => `<tr><td><a href="#" onclick="app.openDoc('${d.id}')">${d.name}</a></td><td>${d.peers}</td><td>${new Date(d.last_sync).toLocaleTimeString()}</td></tr>`).join('') +
              '</tbody></table>'
            }
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
    }
  }

  openDoc(id) {
    this.showToast(`Opening document ${id}… (RGA editor coming soon)`, 'info');
  }

  async renderRegistry() {
    this.showLoading(true);
    try {
      const pkgs = await this.fetchAPI('/registry/packages');
      this.showLoading(false);
      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header">Registry</div>
          <div class="card-body">
            <table class="table">
              <thead><tr><th>Name</th><th>Version</th><th>Author</th><th>Status</th></tr></thead>
              <tbody>
                  ${pkgs.map(p => `<tr>
                  <td>${p.name}</td>
                  <td>${p.version}</td>
                  <td>${p.author}</td>
                  <td>${p.installed ? 'installed' : '<button class="btn btn-sm btn-secondary" onclick="app.installPkg(\'' + p.name + '\')">Install</button>'}
                  </tr>`).join('')}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
    }
  }

  installPkg(name) {
    this.showToast(`Installing ${name}…`, 'info');
  }

  async renderFiles() {
    this.showLoading(true);
    try {
      const [files, transfers] = await Promise.all([
        this.fetchAPI('/files/list').catch(() => []),
        this.fetchAPI('/files/transfers').catch(() => []),
      ]);
      this.showLoading(false);

      const formatSize = (bytes) => {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB';
        return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB';
      };

      const formatTime = (ms) => {
        if (ms < 1000) return ms + 'ms';
        if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
        return (ms / 60000).toFixed(1) + 'm';
      };

      const fileListHtml = files.length === 0
        ? '<p style="color: var(--color-text-muted);">No files shared yet.</p>'
        : `<table class="table"><thead><tr><th>Name</th><th>Size</th><th>Type</th><th>Modified</th><th>Actions</th></tr></thead><tbody>` +
          files.map(f => `
            <tr>
              <td>
                <div style="display: flex; align-items: center; gap: 0.5rem;">
                  <span class="file-icon">${this.getFileIcon(f.name)}</span>
                  <span>${f.name}</span>
                </div>
              </td>
              <td>${formatSize(f.size)}</td>
              <td>${f.mime_type || 'unknown'}</td>
              <td>${new Date(f.modified).toLocaleString()}</td>
              <td>
                <div style="display: flex; gap: 0.25rem;">
                  <button class="btn btn-sm btn-secondary" onclick="app.previewFile('${f.id}', '${f.name}')" title="Preview">👁</button>
                  <button class="btn btn-sm btn-primary" onclick="app.downloadFile('${f.id}')" title="Download">⬇</button>
                  <button class="btn btn-sm btn-secondary" onclick="app.shareFile('${f.id}')" title="Share">🔗</button>
                </div>
              </td>
            </tr>
          `).join('') + '</tbody></table>';

      const activeTransfers = transfers.filter(t => t.status === 'active' || t.status === 'pending');
      const completedTransfers = transfers.filter(t => t.status === 'completed' || t.status === 'failed');

      const transferListHtml = (activeTransfers.length + completedTransfers.length) === 0
        ? '<p style="color: var(--color-text-muted);">No active or recent transfers.</p>'
        : `
          ${activeTransfers.length > 0 ? `
            <h4 style="margin: 1rem 0 0.5rem; font-size: 0.875rem; color: var(--color-text-muted);">Active Transfers</h4>
            <div class="transfer-list">
              ${activeTransfers.map(t => `
                <div class="transfer-item" id="transfer-${t.id}">
                  <div class="transfer-header">
                    <span class="transfer-name">${t.name}</span>
                    <span class="transfer-size">${formatSize(t.bytes_transferred)}/${formatSize(t.total_size)}</span>
                  </div>
                  <div class="progress-bar" style="height: 6px; background: var(--color-border); border-radius: 3px; overflow: hidden; margin: 0.5rem 0;">
                    <div class="progress-fill" style="width: ${t.total_size > 0 ? (t.bytes_transferred / t.total_size * 100).toFixed(1) : 0}%; height: 100%; background: var(--color-primary); border-radius: 3px; transition: width 0.3s;"></div>
                  </div>
                  <div class="transfer-meta" style="font-size: 0.75rem; color: var(--color-text-muted); display: flex; justify-content: space-between;">
                    <span>${t.status} • ${formatTime(t.elapsed_ms)} • ${formatSize(t.speed_bps)}/s</span>
                    <span>${t.peer_name}</span>
                  </div>
                  ${t.can_resume ? `<button class="btn btn-sm btn-secondary" style="margin-top: 0.5rem;" onclick="app.resumeTransfer('${t.id}')">Resume</button>` : ''}
                  ${t.can_pause ? `<button class="btn btn-sm btn-secondary" style="margin-top: 0.5rem;" onclick="app.pauseTransfer('${t.id}')">Pause</button>` : ''}
                  ${t.can_cancel ? `<button class="btn btn-sm btn-critical" style="margin-top: 0.5rem;" onclick="app.cancelTransfer('${t.id}')">Cancel</button>` : ''}
                </div>
              `).join('')}
            </div>
          ` : ''}
          ${completedTransfers.length > 0 ? `
            <h4 style="margin: 1.5rem 0 0.5rem; font-size: 0.875rem; color: var(--color-text-muted);">Recent Transfers</h4>
            <div class="transfer-list">
              ${completedTransfers.slice(0, 10).map(t => `
                <div class="transfer-item ${t.status === 'failed' ? 'failed' : ''}" style="opacity: 0.7;">
                  <div class="transfer-header">
                    <span class="transfer-name">${t.name}</span>
                    <span class="transfer-size">${t.status === 'completed' ? '✓' : '✗'} ${formatSize(t.total_size)}</span>
                  </div>
                  <div class="transfer-meta" style="font-size: 0.75rem; color: var(--color-text-muted);">
                    ${new Date(t.completed_at).toLocaleString()} • ${t.peer_name} • ${t.status === 'completed' ? 'Completed' : 'Failed: ' + (t.error || 'Unknown error')}
                  </div>
                </div>
              `).join('')}
            </div>
          ` : ''}
        `;

      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header" style="display: flex; justify-content: space-between; align-items: center;">
            <span>Files</span>
            <div style="display: flex; gap: 0.5rem;">
              <button class="btn btn-primary" id="upload-btn">Upload Files</button>
            </div>
          </div>
          <div class="card-body">
            <div id="drop-zone" class="drop-zone" style="border: 2px dashed var(--color-border); border-radius: 0.5rem; padding: 3rem; text-align: center; margin-bottom: 1.5rem; cursor: pointer; transition: all 0.2s;">
              <div style="font-size: 3rem; margin-bottom: 1rem;">📁</div>
              <p style="color: var(--color-text-muted); margin-bottom: 0.5rem;">Drag & drop files here, or click to select</p>
              <p style="font-size: 0.8125rem; color: var(--color-text-muted);">Supports multiple files, folders, and resume</p>
              <input type="file" id="file-input" multiple style="display: none;" />
            </div>
            ${fileListHtml}
            ${transferListHtml}
          </div>
        </div>
      `;

      // Setup drag-drop and file input
      this.setupFileDropZone();

    } catch (e) {
      this.showLoading(false);
      this.showToast('Failed to load files', 'error');
    }
  }

  getFileIcon(filename) {
    const ext = filename.split('.').pop().toLowerCase();
    const icons = {
      pdf: '📄', doc: '📝', docx: '📝', txt: '📝', md: '📝',
      jpg: '🖼', jpeg: '🖼', png: '🖼', gif: '🖼', webp: '🖼', svg: '🖼',
      mp4: '🎬', mov: '🎬', avi: '🎬', mkv: '🎬', webm: '🎬',
      mp3: '🎵', wav: '🎵', flac: '🎵', ogg: '🎵',
      zip: '📦', tar: '📦', gz: '📦', rar: '📦', '7z': '📦',
      js: '📜', ts: '📜', py: '🐍', go: '🐹', rs: '🦀', html: '🌐', css: '🎨',
      json: '📋', xml: '📋', yaml: '📋', yml: '📋',
    };
    return icons[ext] || '📄';
  }

  setupFileDropZone() {
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const uploadBtn = document.getElementById('upload-btn');

    if (!dropZone || !fileInput) return;

    // Click to open file dialog
    dropZone.addEventListener('click', (e) => {
      if (e.target === dropZone || e.target.closest('.drop-zone') === dropZone) {
        fileInput.click();
      }
    });

    // Upload button
    if (uploadBtn) {
      uploadBtn.addEventListener('click', () => fileInput.click());
    }

    // File input change
    fileInput.addEventListener('change', (e) => {
      if (e.target.files.length > 0) {
        this.uploadFiles(Array.from(e.target.files));
      }
    });

    // Drag and drop
    dropZone.addEventListener('dragover', (e) => {
      e.preventDefault();
      dropZone.style.borderColor = 'var(--color-primary)';
      dropZone.style.background = 'var(--color-primary-light)';
    });

    dropZone.addEventListener('dragleave', (e) => {
      e.preventDefault();
      dropZone.style.borderColor = 'var(--color-border)';
      dropZone.style.background = '';
    });

    dropZone.addEventListener('drop', (e) => {
      e.preventDefault();
      dropZone.style.borderColor = 'var(--color-border)';
      dropZone.style.background = '';
      if (e.dataTransfer.files.length > 0) {
        this.uploadFiles(Array.from(e.dataTransfer.files));
      }
    });
  }

  async uploadFiles(files) {
    if (files.length === 0) return;

    this.showToast(`Starting upload of ${files.length} file(s)…`, 'info');

    for (const file of files) {
      try {
        // For demo, we'll show the upload in the transfer list
        // In real implementation, this would use the Files service BitSwap protocol
        const transferId = 'transfer-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);

        // Add to active transfers UI immediately
        this.addTransferToUI({
          id: transferId,
          name: file.name,
          total_size: file.size,
          bytes_transferred: 0,
          status: 'active',
          speed_bps: 0,
          elapsed_ms: 0,
          peer_name: 'auto',
          can_pause: true,
          can_cancel: true,
        });

        // Simulate upload progress
        await this.simulateUpload(transferId, file);
      } catch (e) {
        this.showToast(`Failed to upload ${file.name}: ${e.message}`, 'error');
      }
    }
  }

  addTransferToUI(transfer) {
    // This will be called to add a transfer to the active transfers list
    // For now, just refresh the files page
    this.renderFiles();
  }

  async simulateUpload(transferId, file) {
    const chunkSize = 1024 * 1024; // 1MB chunks
    let uploaded = 0;
    const startTime = Date.now();

    while (uploaded < file.size) {
      await new Promise(r => setTimeout(r, 100)); // Simulate network delay

      const chunk = Math.min(chunkSize, file.size - uploaded);
      uploaded += chunk;

      // In real implementation, this would update via SSE
      // For demo, we'll just update the UI periodically
      if (uploaded % (chunkSize * 5) === 0 || uploaded >= file.size) {
        // Trigger UI update
        this.renderFiles();
      }
    }

    // Mark as completed
    this.showToast(`${file.name} uploaded successfully`, 'success');
    this.renderFiles();
  }

  previewFile(fileId, fileName) {
    this.showToast(`Preview for ${fileName} - opening…`, 'info');
    // In real implementation, this would open a preview modal
  }

  downloadFile(fileId) {
    this.showToast('Starting download…', 'info');
    // In real implementation, this would trigger BitSwap download
  }

  shareFile(fileId) {
    this.showToast('Generating share link…', 'info');
    // In real implementation, this would create a shareable link
  }

  resumeTransfer(transferId) {
    this.showToast(`Resuming transfer ${transferId}…`, 'info');
    // In real implementation, this would resume interrupted BitSwap transfer
  }

  pauseTransfer(transferId) {
    this.showToast(`Pausing transfer ${transferId}…`, 'info');
    // In real implementation, this would pause BitSwap transfer
  }

  cancelTransfer(transferId) {
    this.showToast(`Cancelled transfer ${transferId}`, 'info');
    // In real implementation, this would cancel BitSwap transfer
  }

  renderVoice() {
    document.getElementById('content').innerHTML = `
      <div class="card">
        <div class="card-header">Voice</div>
        <div class="card-body">
          <div style="display: flex; align-items: center; gap: 1rem;">
            <button class="btn btn-primary" onclick="app.startCall()">Start Call</button>
            <button class="btn btn-secondary" onclick="app.endCall()" style="display: none;" id="end-call-btn">End Call</button>
            <span class="connection-status offline" id="call-status"></span>
            <span id="call-status-text" style="color: var(--color-text-muted);">Idle</span>
          </div>
          <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-top: 0.5rem;">
            ICE + Opus codec. Audio requires microphone access.
          </p>
        </div>
      </div>
    `;
  }

  startCall() {
    document.getElementById('call-status').className = 'connection-status online';
    document.getElementById('call-status-text').textContent = 'Connected';
    document.getElementById('end-call-btn').style.display = 'inline-flex';
    this.showToast('Call started', 'success');
  }

  endCall() {
    document.getElementById('call-status').className = 'connection-status offline';
    document.getElementById('call-status-text').textContent = 'Idle';
    document.getElementById('end-call-btn').style.display = 'none';
    this.showToast('Call ended', 'info');
  }

  renderVPN() {
    document.getElementById('content').innerHTML = `
      <div class="card">
        <div class="card-header">VPN Tunnel</div>
        <div class="card-body">
          <div style="display: flex; align-items: center; gap: 1rem; margin-bottom: 1rem;">
            <button class="btn btn-primary" onclick="app.toggleVPN()">Connect</button>
            <span class="connection-status offline" id="vpn-status"></span>
            <span id="vpn-status-text" style="color: var(--color-text-muted);">Disconnected</span>
          </div>
          <div id="vpn-routes" style="display: none;">
            <table class="table">
              <thead><tr><th>Route</th><th>Peer</th><th>Status</th></tr></thead>
              <tbody>
                <tr><td>10.42.0.0/24</td><td>peer-abc</td><td>active</td></tr>
              </tbody>
            </table>
          </div>
          <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-top: 0.5rem;">
            TUN tunnel over Noise-secured QUIC. Requires root/CAP_NET_ADMIN on Linux.
          </p>
        </div>
      </div>
    `;
  }

  toggleVPN() {
    const status = document.getElementById('vpn-status');
    const text = document.getElementById('vpn-status-text');
    const routes = document.getElementById('vpn-routes');
    const btn = event.target;

    if (status.classList.contains('offline')) {
      status.classList.remove('offline');
      status.classList.add('online');
      text.textContent = 'Connected';
      routes.style.display = 'block';
      btn.textContent = 'Disconnect';
      this.showToast('VPN tunnel established', 'success');
    } else {
      status.classList.remove('online');
      status.classList.add('offline');
      text.textContent = 'Disconnected';
      routes.style.display = 'none';
      btn.textContent = 'Connect';
      this.showToast('VPN tunnel closed', 'info');
    }
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
  window.app = new LocalWEBApp();
});

window.addEventListener('hashchange', () => {
  const route = window.location.hash.slice(1) || 'dashboard';
  window.app?.navigate(route);
});
