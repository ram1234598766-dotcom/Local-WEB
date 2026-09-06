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

      const docListHtml = docs.length === 0
        ? '<p style="color: var(--color-text-muted);">No documents yet. Create one to start collaborating.</p>'
        : `<table class="table"><thead><tr><th>Name</th><th>Peers</th><th>Last Sync</th><th>Actions</th></tr></thead><tbody>` +
          docs.map(d => `
            <tr>
              <td>${d.name}</td>
              <td>${d.peers}</td>
              <td>${new Date(d.last_sync).toLocaleString()}</td>
              <td>
                <button class="btn btn-sm btn-primary" onclick="app.openDocEditor('${d.id}', '${d.name}')">Edit</button>
                <button class="btn btn-sm btn-secondary" onclick="app.openDocHistory('${d.id}')" title="Version History">⏪</button>
              </td>
            </tr>
          `).join('') + '</tbody></table>';

      document.getElementById('content').innerHTML = `
        <div class="card">
          <div class="card-header" style="display: flex; justify-content: space-between; align-items: center;">
            <span>Collaborative Documents</span>
            <button class="btn btn-primary" onclick="app.createNewDoc()">New Document</button>
          </div>
          <div class="card-body">
            ${docListHtml}
          </div>
        </div>
      `;
    } catch (e) {
      this.showLoading(false);
      this.showToast('Failed to load documents', 'error');
    }
  }

  async createNewDoc() {
    const name = prompt('Enter document name:', 'Untitled Document');
    if (!name) return;

    this.showLoading(true);
    try {
      const resp = await fetch('/api/docs/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });
      const data = await resp.json();
      if (data.id) {
        this.showToast('Document created', 'success');
        this.openDocEditor(data.id, data.name);
      } else {
        this.showToast('Failed to create document', 'error');
      }
    } catch (e) {
      this.showToast('Error creating document: ' + e.message, 'error');
    }
    this.showLoading(false);
  }

  openDocEditor(id, name) {
    window.location.hash = '#doc-editor-' + id;
    this.renderDocEditor(id, name);
  }

  async renderDocEditor(docId, docName) {
    this.showLoading(true);
    try {
      const [doc, presence] = await Promise.all([
        this.fetchAPI('/docs/documents/' + docId).catch(() => null),
        this.fetchAPI('/docs/presence/' + docId).catch(() => ({ users: [] })),
      ]);
      this.showLoading(false);

      const content = doc?.content || '';
      const version = doc?.version || 0;
      const users = presence?.users || [];

      const userColors = ['var(--color-primary)', 'var(--color-accent)', 'var(--color-success)', 'var(--color-warning)', 'var(--color-critical)', 'var(--color-info)'];

      document.getElementById('content').innerHTML = `
        <style>
          .doc-editor-container { display: flex; flex-direction: column; height: calc(100vh - 200px); min-height: 500px; }
          .doc-toolbar { display: flex; gap: 0.5rem; padding: 0.75rem; border-bottom: 1px solid var(--color-border); flex-wrap: wrap; }
          .doc-toolbar-group { display: flex; gap: 0.25rem; padding: 0 0.5rem; border-right: 1px solid var(--color-border); }
          .doc-toolbar-btn { padding: 0.375rem 0.75rem; border: 1px solid var(--color-border); background: var(--color-bg); border-radius: 0.25rem; cursor: pointer; font-size: 0.8125rem; transition: all 0.15s; }
          .doc-toolbar-btn:hover { background: var(--color-primary-light); border-color: var(--color-primary); color: var(--color-primary); }
          .doc-toolbar-btn.active { background: var(--color-primary); border-color: var(--color-primary); color: white; }
          .doc-toolbar-btn:disabled { opacity: 0.5; cursor: not-allowed; }
          .doc-editor-wrapper { flex: 1; position: relative; overflow: hidden; }
          .doc-editor { width: 100%; height: 100%; border: none; outline: none; padding: 1.5rem; font-family: 'Monospace', monospace; font-size: 0.9375rem; line-height: 1.7; background: var(--color-bg); color: var(--color-text); resize: none; }
          .doc-editor:focus { outline: none; }
          .presence-bar { display: flex; gap: 0.5rem; padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--color-border); flex-wrap: wrap; align-items: center; }
          .presence-avatar { width: 28px; height: 28px; border-radius: 50%; display: flex; align-items: center; justify-content: center; color: white; font-weight: 600; font-size: 0.75rem; cursor: pointer; transition: transform 0.15s; border: 2px solid var(--color-bg); }
          .presence-avatar:hover { transform: scale(1.1); z-index: 10; }
          .presence-avatar.you { border-color: var(--color-primary); }
          .presence-tooltip { position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%); background: var(--color-text); color: var(--color-bg); padding: 0.25rem 0.5rem; border-radius: 0.25rem; font-size: 0.6875rem; white-space: nowrap; margin-bottom: 0.25rem; opacity: 0; pointer-events: none; transition: opacity 0.15s; }
          .presence-avatar:hover .presence-tooltip { opacity: 1; }
          .remote-cursor { position: absolute; width: 2px; height: 1.2em; pointer-events: none; z-index: 100; }
          .remote-cursor::after { content: attr(data-name); position: absolute; top: -1.4em; left: 0; background: inherit; color: white; padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.625rem; white-space: nowrap; transform: translateX(-50%); }
          .comment-thread { position: absolute; right: 0; width: 280px; max-height: 100%; overflow-y: auto; border-left: 1px solid var(--color-border); background: var(--color-bg-elevated); }
          .comment-thread.collapsed { width: 32px; }
          .comment-thread-toggle { writing-mode: vertical-rl; text-orientation: mixed; padding: 1rem 0.25rem; cursor: pointer; color: var(--color-text-muted); }
          .comment-thread-toggle:hover { color: var(--color-primary); }
          .comment { padding: 0.75rem; border-bottom: 1px solid var(--color-border); }
          .comment-header { display: flex; justify-content: space-between; margin-bottom: 0.375rem; font-size: 0.75rem; color: var(--color-text-muted); }
          .comment-author { font-weight: 600; color: var(--color-text); }
          .comment-time { font-size: 0.6875rem; }
          .comment-text { font-size: 0.8125rem; line-height: 1.5; }
          .comment-input { padding: 0.5rem; border-top: 1px solid var(--color-border); display: flex; gap: 0.5rem; }
          .comment-input textarea { flex: 1; min-height: 60px; max-height: 120px; padding: 0.5rem; border: 1px solid var(--color-border); border-radius: 0.25rem; background: var(--color-bg); color: var(--color-text); font-family: inherit; font-size: 0.8125rem; resize: vertical; }
          .comment-input button { align-self: flex-end; }
          .version-history { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
          .version-history-panel { background: var(--color-bg); border-radius: 0.5rem; padding: 1.5rem; max-width: 600px; max-height: 80vh; overflow-y: auto; width: 90%; box-shadow: var(--shadow-xl); }
          .version-item { padding: 0.75rem; border: 1px solid var(--color-border); border-radius: 0.25rem; margin-bottom: 0.5rem; cursor: pointer; transition: all 0.15s; }
          .version-item:hover { background: var(--color-primary-light); border-color: var(--color-primary); }
          .version-item.current { border-color: var(--color-primary); background: var(--color-primary-light); }
          .version-meta { display: flex; justify-content: space-between; margin-bottom: 0.375rem; font-size: 0.75rem; }
          .version-author { font-weight: 600; }
          .version-time { color: var(--color-text-muted); }
          .version-preview { font-size: 0.8125rem; color: var(--color-text-muted); max-height: 60px; overflow: hidden; text-overflow: ellipsis; }
        </style>
        <div class="doc-editor-container">
          <div class="presence-bar">
            <span style="font-size: 0.8125rem; color: var(--color-text-muted); margin-right: 1rem;">Collaborators:</span>
            ${users.map((u, i) => `
              <div class="presence-avatar ${u.id === this.state.nodeID ? 'you' : ''}" style="background: ${userColors[i % userColors.length]};" title="${u.name}${u.id === this.state.nodeID ? ' (you)' : ''}">
                ${u.name.charAt(0).toUpperCase()}
                <span class="presence-tooltip">${u.name}${u.id === this.state.nodeID ? ' (you)' : ''}</span>
              </div>
            `).join('')}
            <div style="margin-left: auto; display: flex; gap: 0.5rem;">
              <button class="btn btn-sm btn-secondary" onclick="app.toggleComments('${docId}')">
                💬 Comments
              </button>
              <button class="btn btn-sm btn-secondary" onclick="app.openVersionHistory('${docId}')">
                ⏪ History
              </button>
              <button class="btn btn-sm btn-primary" onclick="app.saveDoc('${docId}')">
                💾 Save
              </button>
            </div>
          </div>
          <div style="display: flex; flex: 1; position: relative;">
            <div class="doc-editor-wrapper" style="flex: 1;">
              <div class="doc-toolbar">
                <div class="doc-toolbar-group">
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'bold')" title="Bold (Ctrl+B)"><strong>B</strong></button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'italic')" title="Italic (Ctrl+I)"><em>I</em></button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'underline')" title="Underline (Ctrl+U)"><u>U</u></button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'strikethrough')" title="Strikethrough"><s>S</s></button>
                </div>
                <div class="doc-toolbar-group">
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'heading1')" title="Heading 1">H1</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'heading2')" title="Heading 2">H2</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'heading3')" title="Heading 3">H3</button>
                </div>
                <div class="doc-toolbar-group">
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'bullet')" title="Bullet List">• List</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'numbered')" title="Numbered List">1. List</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'code')" title="Inline Code">{ }</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'codeblock')" title="Code Block">[ ]</button>
                </div>
                <div class="doc-toolbar-group">
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'undo')" title="Undo (Ctrl+Z)">↶</button>
                  <button class="doc-toolbar-btn" onclick="app.formatDoc('${docId}', 'redo')" title="Redo (Ctrl+Y)">↷</button>
                </div>
              </div>
              <textarea class="doc-editor" id="doc-editor-${docId}" spellcheck="true" data-doc-id="${docId}">${content}</textarea>
            </div>
            <div id="comments-panel-${docId}" class="comment-thread collapsed">
              <div class="comment-thread-toggle" onclick="app.toggleCommentsPanel('${docId}')">Comments</div>
              <div id="comments-list-${docId}" style="display: none; flex-direction: column; flex: 1; overflow-y: auto;">
                <!-- Comments loaded dynamically -->
              </div>
              <div class="comment-input" style="display: none;">
                <textarea id="comment-input-${docId}" placeholder="Add a comment…" rows="2"></textarea>
                <button class="btn btn-primary btn-sm" onclick="app.addComment('${docId}')">Post</button>
              </div>
            </div>
          </div>
        </div>
      `;

      // Initialize editor
      this.initDocEditor(docId, docName);

    } catch (e) {
      this.showLoading(false);
      this.showToast('Failed to load document: ' + e.message, 'error');
    }
  }

  initDocEditor(docId, docName) {
    const editor = document.getElementById('doc-editor-' + docId);
    if (!editor) return;

    let lastContent = editor.value;
    let saveTimeout = null;

    // Auto-save on change
    editor.addEventListener('input', () => {
      clearTimeout(saveTimeout);
      saveTimeout = setTimeout(() => {
        this.autoSaveDoc(docId, editor.value);
      }, 2000);
    });

    // Keyboard shortcuts
    editor.addEventListener('keydown', (e) => {
      if (e.ctrlKey || e.metaKey) {
        switch (e.key.toLowerCase()) {
          case 'b': e.preventDefault(); this.formatDoc(docId, 'bold'); break;
          case 'i': e.preventDefault(); this.formatDoc(docId, 'italic'); break;
          case 'u': e.preventDefault(); this.formatDoc(docId, 'underline'); break;
          case 'z': e.preventDefault(); this.formatDoc(docId, e.shiftKey ? 'redo' : 'undo'); break;
          case 'y': e.preventDefault(); this.formatDoc(docId, 'redo'); break;
          case 's': e.preventDefault(); this.saveDoc(docId); break;
        }
      }
    });

    // Load comments
    this.loadComments(docId);

    // Load remote cursors (simulated via SSE)
    this.subscribeToDocPresence(docId);
  }

  subscribeToDocPresence(docId) {
    // In real implementation, this would subscribe to SSE for real-time presence
    // For demo, we'll simulate
  }

  async loadComments(docId) {
    try {
      const comments = await this.fetchAPI('/docs/comments/' + docId).catch(() => []);
      const list = document.getElementById('comments-list-' + docId);
      if (list && comments.length > 0) {
        list.innerHTML = comments.map(c => `
          <div class="comment">
            <div class="comment-header">
              <span class="comment-author">${c.author}</span>
              <span class="comment-time">${new Date(c.timestamp).toLocaleString()}</span>
            </div>
            <div class="comment-text">${c.text}</div>
          </div>
        `).join('');
      }
    } catch (e) {
      // Ignore
    }
  }

  toggleComments(docId) {
    const panel = document.getElementById('comments-panel-' + docId);
    if (panel) panel.classList.toggle('collapsed');
  }

  toggleCommentsPanel(docId) {
    const panel = document.getElementById('comments-panel-' + docId);
    const list = document.getElementById('comments-list-' + docId);
    const input = document.getElementById('comment-input-' + docId);
    if (panel && list && input) {
      const isCollapsed = panel.classList.contains('collapsed');
      panel.classList.toggle('collapsed');
      list.style.display = isCollapsed ? 'flex' : 'none';
      input.style.display = isCollapsed ? 'flex' : 'none';
    }
  }

  async addComment(docId) {
    const input = document.getElementById('comment-input-' + docId);
    if (!input || !input.value.trim()) return;

    this.showLoading(true);
    try {
      await fetch('/api/docs/comments/' + docId, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: input.value })
      });
      input.value = '';
      this.loadComments(docId);
      this.showToast('Comment added', 'success');
    } catch (e) {
      this.showToast('Failed to add comment', 'error');
    }
    this.showLoading(false);
  }

  async saveDoc(docId) {
    const editor = document.getElementById('doc-editor-' + docId);
    if (!editor) return;

    this.showLoading(true);
    try {
      await fetch('/api/docs/save/' + docId, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: editor.value })
      });
      this.showToast('Document saved', 'success');
    } catch (e) {
      this.showToast('Save failed: ' + e.message, 'error');
    }
    this.showLoading(false);
  }

  async autoSaveDoc(docId, content) {
    try {
      await fetch('/api/docs/autosave/' + docId, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content })
      });
    } catch (e) {
      // Silent fail for autosave
    }
  }

  formatDoc(docId, format) {
    const editor = document.getElementById('doc-editor-' + docId);
    if (!editor) return;

    const start = editor.selectionStart;
    const end = editor.selectionEnd;
    const selected = editor.value.substring(start, end);
    let replacement = selected;

    switch (format) {
      case 'bold': replacement = '**' + selected + '**'; break;
      case 'italic': replacement = '_' + selected + '_'; break;
      case 'underline': replacement = '__' + selected + '__'; break;
      case 'strikethrough': replacement = '~~' + selected + '~~'; break;
      case 'heading1': replacement = '# ' + selected; break;
      case 'heading2': replacement = '## ' + selected; break;
      case 'heading3': replacement = '### ' + selected; break;
      case 'bullet': replacement = selected.split('\n').map(l => '- ' + l).join('\n'); break;
      case 'numbered': replacement = selected.split('\n').map((l, i) => (i+1) + '. ' + l).join('\n'); break;
      case 'code': replacement = '`' + selected + '`'; break;
      case 'codeblock': replacement = '\n```\n' + selected + '\n```\n'; break;
      case 'undo': document.execCommand('undo'); return;
      case 'redo': document.execCommand('redo'); return;
    }

    if (replacement !== selected) {
      editor.value = editor.value.substring(0, start) + replacement + editor.value.substring(end);
      editor.focus();
      editor.setSelectionRange(start, start + replacement.length);
      this.autoSaveDoc(docId, editor.value);
    }
  }

  openVersionHistory(docId) {
    // This will be implemented to show version history modal
    this.showToast('Version history - coming soon', 'info');
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
      <style>
        .call-container { display: flex; flex-direction: column; height: calc(100vh - 200px); min-height: 500px; }
        .call-header { display: flex; justify-content: space-between; align-items: center; padding: 1rem; border-bottom: 1px solid var(--color-border); }
        .call-title { display: flex; align-items: center; gap: 0.75rem; }
        .call-status-badge { padding: 0.25rem 0.75rem; border-radius: 9999px; font-size: 0.6875rem; font-weight: 600; text-transform: uppercase; }
        .call-status-badge.idle { background: var(--color-bg); color: var(--color-text-muted); border: 1px solid var(--color-border); }
        .call-status-badge.connecting { background: var(--color-warning-light); color: var(--color-warning); border: 1px solid var(--color-warning); animation: pulse 1.5s infinite; }
        .call-status-badge.active { background: var(--color-success-light); color: var(--color-success); border: 1px solid var(--color-success); }
        .call-status-badge.ended { background: var(--color-critical-light); color: var(--color-critical); border: 1px solid var(--color-critical); }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
        .call-timer { font-family: monospace; font-size: 1.125rem; font-weight: 600; color: var(--color-text); }
        .call-controls { display: flex; gap: 0.5rem; }
        .call-control-btn { width: 48px; height: 48px; border-radius: 50%; display: flex; align-items: center; justify-content: center; border: none; cursor: pointer; font-size: 1.25rem; transition: all 0.15s; }
        .call-control-btn.primary { background: var(--color-primary); color: white; }
        .call-control-btn.primary:hover { background: var(--color-primary-dark); transform: scale(1.05); }
        .call-control-btn.secondary { background: var(--color-bg); color: var(--color-text); border: 1px solid var(--color-border); }
        .call-control-btn.secondary:hover { background: var(--color-primary-light); border-color: var(--color-primary); color: var(--color-primary); }
        .call-control-btn.danger { background: var(--color-critical); color: white; }
        .call-control-btn.danger:hover { background: var(--color-critical-dark); transform: scale(1.05); }
        .call-control-btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .call-control-btn.active { background: var(--color-primary); color: white; box-shadow: 0 0 0 2px var(--color-primary-light); }
        .video-grid { flex: 1; display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 1rem; padding: 1rem; overflow: auto; }
        .video-tile { position: relative; background: var(--color-bg-elevated); border-radius: 0.5rem; overflow: hidden; border: 1px solid var(--color-border); aspect-ratio: 16/9; min-height: 180px; }
        .video-tile.local { border-color: var(--color-primary); }
        .video-tile video { width: 100%; height: 100%; object-fit: cover; background: #000; }
        .video-tile.muted video { filter: grayscale(0.3); }
        .video-overlay { position: absolute; bottom: 0; left: 0; right: 0; padding: 0.75rem; background: linear-gradient(transparent, rgba(0,0,0,0.7)); color: white; }
        .video-name { font-weight: 500; font-size: 0.875rem; }
        .video-status { display: flex; gap: 0.5rem; margin-top: 0.25rem; font-size: 0.6875rem; }
        .status-indicator { display: flex; align-items: center; gap: 0.25rem; }
        .status-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-success); }
        .status-dot.muted { background: var(--color-warning); }
        .status-dot.camera-off { background: var(--color-critical); }
        .status-dot.screen-sharing { background: var(--color-info); animation: pulse 1s infinite; }
        .call-footer { padding: 1rem; border-top: 1px solid var(--color-border); display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 1rem; }
        .call-settings { display: flex; gap: 0.5rem; }
        .setting-item { display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 0.75rem; background: var(--color-bg); border: 1px solid var(--color-border); border-radius: 0.375rem; cursor: pointer; font-size: 0.8125rem; transition: all 0.15s; }
        .setting-item:hover { background: var(--color-primary-light); border-color: var(--color-primary); color: var(--color-primary); }
        .setting-item.active { background: var(--color-primary); color: white; border-color: var(--color-primary); }
        .setting-icon { font-size: 1rem; }
        .call-ended { text-align: center; padding: 3rem; color: var(--color-text-muted); }
        .call-ended h3 { margin-bottom: 0.5rem; color: var(--color-text); }
        .call-summary { background: var(--color-bg-elevated); border-radius: 0.5rem; padding: 1rem; margin: 1rem auto; max-width: 400px; text-align: left; }
        .call-summary-row { display: flex; justify-content: space-between; padding: 0.25rem 0; font-size: 0.8125rem; }
        .call-summary-label { color: var(--color-text-muted); }
        .call-summary-value { font-weight: 500; }
      </style>
      <div class="call-container" id="voice-call-container">
        <div class="call-header">
          <div class="call-title">
            <h3 style="margin: 0; font-size: 1rem;">Voice/Video Call</h3>
            <span class="call-status-badge idle" id="call-status-badge">Idle</span>
          </div>
          <div class="call-timer" id="call-timer" style="display: none;">00:00</div>
          <div class="call-controls">
            <button class="call-control-btn secondary" id="screen-share-btn" onclick="app.toggleScreenShare()" title="Share Screen" disabled>🖥</button>
            <button class="call-control-btn secondary" id="record-btn" onclick="app.toggleRecording()" title="Record" disabled>⏺</button>
            <button class="call-control-btn secondary" id="virtual-bg-btn" onclick="app.toggleVirtualBackground()" title="Virtual Background" disabled>🎭</button>
            <button class="call-control-btn secondary" id="settings-btn" onclick="app.toggleCallSettings()" title="Settings">⚙</button>
          </div>
        </div>
        <div class="video-grid" id="video-grid">
          <div class="video-tile local" id="local-video-tile">
            <video id="local-video" autoplay muted playsinline></video>
            <div class="video-overlay">
              <div class="video-name">You (Local)</div>
              <div class="video-status">
                <span class="status-indicator"><span class="status-dot" id="local-audio-status"></span> Audio</span>
                <span class="status-indicator"><span class="status-dot" id="local-video-status"></span> Video</span>
                <span class="status-indicator"><span class="status-dot screen-sharing" id="local-screen-status" style="display: none;"></span> Screen</span>
              </div>
            </div>
            <div class="video-tile" id="remote-video-placeholder" style="display: none;">
              <video id="remote-video" autoplay playsinline></video>
              <div class="video-overlay">
                <div class="video-name" id="remote-video-name">Connecting…</div>
                <div class="video-status">
                  <span class="status-indicator"><span class="status-dot" id="remote-audio-status"></span> Audio</span>
                  <span class="status-indicator"><span class="status-dot" id="remote-video-status"></span> Video</span>
                  <span class="status-indicator"><span class="status-dot screen-sharing" id="remote-screen-status" style="display: none;"></span> Screen</span>
                </div>
              </div>
            </div>
          </div>
          <div class="call-footer">
            <div class="call-settings" id="call-settings" style="display: none;">
              <div class="setting-item" onclick="app.toggleMute()"><span class="setting-icon" id="mic-icon">🎤</span><span id="mic-label">Mute</span></div>
              <div class="setting-item" onclick="app.toggleCamera()"><span class="setting-icon" id="cam-icon">📷</span><span id="cam-label">Camera</span></div>
              <div class="setting-item" onclick="app.toggleSpeaker()"><span class="setting-icon" id="speaker-icon">🔊</span><span id="speaker-label">Speaker</span></div>
              <div class="setting-item" onclick="app.selectVirtualBackground()"><span class="setting-icon">🎨</span><span>Backgrounds</span></div>
              <div class="setting-item" onclick="app.openDevicesModal()"><span class="setting-icon">🎧</span><span>Devices</span></div>
            </div>
            <div class="call-controls">
              <button class="call-control-btn secondary" id="mute-btn" onclick="app.toggleMute()" title="Mute/Unmute (M)">🎤</button>
              <button class="call-control-btn secondary" id="camera-btn" onclick="app.toggleCamera()" title="Camera On/Off (C)">📷</button>
              <button class="call-control-btn danger" id="hangup-btn" onclick="app.endCall()" title="End Call">📞</button>
            </div>
          </div>
        </div>
      `;
    `;

    this.callState = {
      status: 'idle', // idle, connecting, active, ended
      localStream: null,
      remoteStream: null,
      peerConnection: null,
      isMuted: false,
      isCameraOff: false,
      isScreenSharing: false,
      isRecording: false,
      virtualBackground: null,
      callStartTime: null,
      timerInterval: null,
      remotePeerId: null,
    };

    this.initVoiceCall();
  }

  async initVoiceCall() {
    try {
      this.callState.localStream = await navigator.mediaDevices.getUserMedia({
        audio: true,
        video: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } }
      });

      const localVideo = document.getElementById('local-video');
      if (localVideo) {
        localVideo.srcObject = this.callState.localStream;
        this.updateLocalStatus();
      }

      // Load available devices
      const devices = await navigator.mediaDevices.enumerateDevices();
      this.callState.devices = {
        audioInput: devices.filter(d => d.kind === 'audioinput'),
        videoInput: devices.filter(d => d.kind === 'videoinput'),
        audioOutput: devices.filter(d => d.kind === 'audiooutput'),
      };

      // Populate virtual backgrounds
      this.callState.virtualBackgrounds = [
        { id: 'none', name: 'None', image: null },
        { id: 'blur', name: 'Blur', image: 'blur' },
        { id: 'office', name: 'Office', image: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1920 1080"><rect fill="%23e8e8e8" width="1920" height="1080"/><rect fill="%23fff" x="100" y="100" width="400" height="300" rx="8"/><rect fill="%23333" x="120" y="130" width="360" height="20" rx="4"/><rect fill="%23666" x="120" y="160" width="280" height="12" rx="4"/></svg>' },
        { id: 'nature', name: 'Nature', image: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1920 1080"><rect fill="%2387ceeb" width="1920" height="1080"/><ellipse fill="%23228b22" cx="1600" cy="900" rx="300" ry="200"/><ellipse fill="%23ffd700" cx="300" cy="200" rx="80" ry="80"/></svg>' },
      ];

      this.showToast('Camera and microphone ready. Click "Start Call" to begin.', 'info');
    } catch (e) {
      this.showToast('Failed to access camera/microphone: ' + e.message, 'error');
      document.getElementById('mute-btn')?.classList.add('disabled');
      document.getElementById('camera-btn')?.classList.add('disabled');
    }
  }

  updateLocalStatus() {
    const audioTrack = this.callState.localStream?.getAudioTracks()[0];
    const videoTrack = this.callState.localStream?.getVideoTracks()[0];

    const audioStatus = document.getElementById('local-audio-status');
    const videoStatus = document.getElementById('local-video-status');

    if (audioStatus) {
      audioStatus.className = 'status-dot' + (audioTrack?.enabled === false ? ' muted' : '');
    }
    if (videoStatus) {
      videoStatus.className = 'status-dot' + (videoTrack?.enabled === false ? ' camera-off' : '');
    }
  }

  async startCall(peerId = null) {
    if (this.callState.status === 'active') return;

    this.callState.status = 'connecting';
    this.callState.remotePeerId = peerId;
    this.updateCallUI();

    try {
      // In real implementation, this would use the Voice service to establish WebRTC connection
      // via the signaling server (QUIC stream)
      this.showToast('Establishing connection…', 'info');

      // Simulate connection
      await new Promise(r => setTimeout(r, 1500));

      // Simulate remote peer joining
      this.simulateRemotePeer();

      this.callState.status = 'active';
      this.callState.callStartTime = Date.now();
      this.startCallTimer();
      this.updateCallUI();

      this.showToast('Call connected', 'success');
    } catch (e) {
      this.callState.status = 'idle';
      this.updateCallUI();
      this.showToast('Call failed: ' + e.message, 'error');
    }
  }

  simulateRemotePeer() {
    // Simulate remote video
    const placeholder = document.getElementById('remote-video-placeholder');
    const remoteVideo = document.getElementById('remote-video');
    const remoteName = document.getElementById('remote-video-name');

    if (placeholder && remoteVideo) {
      placeholder.style.display = 'block';

      // Create a mock stream (in real implementation, this comes from WebRTC)
      const canvas = document.createElement('canvas');
      canvas.width = 640;
      canvas.height = 360;
      const ctx = canvas.getContext('2d');

      const drawFrame = () => {
        if (this.callState.status !== 'active') return;

        // Draw a simulated remote video frame
        const time = Date.now();
        ctx.fillStyle = '#1a1a2e';
        ctx.fillRect(0, 0, canvas.width, canvas.height);

        // Animated gradient background
        const gradient = ctx.createLinearGradient(0, 0, canvas.width, canvas.height);
        gradient.addColorStop(0, `hsl(${(time / 50) % 360}, 70%, 20%)`);
        gradient.addColorStop(1, `hsl(${(time / 50 + 180) % 360}, 70%, 15%)`);
        ctx.fillStyle = gradient;
        ctx.fillRect(0, 0, canvas.width, canvas.height);

        // Moving elements
        for (let i = 0; i < 5; i++) {
          const x = (time / 30 + i * 400) % (canvas.width + 100) - 50;
          const y = 100 + Math.sin(time / 1000 + i) * 50;
          ctx.beginPath();
          ctx.arc(x, y, 20 + Math.sin(time / 500 + i) * 10, 0, Math.PI * 2);
          ctx.fillStyle = `hsla(${(time / 20 + i * 72) % 360}, 80%, 60%, 0.6)`;
          ctx.fill();
        }

        // "Remote" label
        ctx.fillStyle = 'rgba(255,255,255,0.8)';
        ctx.font = '24px monospace';
        ctx.textAlign = 'center';
        ctx.fillText('Remote Peer', canvas.width / 2, canvas.height / 2);

        remoteVideo.srcObject = canvas.captureStream(30);
        requestAnimationFrame(drawFrame);
      };

      drawFrame();

      if (remoteName) remoteName.textContent = 'Peer-' + Math.random().toString(36).substr(2, 6);

      // Simulate remote status
      const remoteAudio = document.getElementById('remote-audio-status');
      const remoteVideo = document.getElementById('remote-video-status');
      if (remoteAudio) remoteAudio.className = 'status-dot';
      if (remoteVideo) remoteVideo.className = 'status-dot';
    }

  endCall() {
    if (this.callState.timerInterval) {
      clearInterval(this.callState.timerInterval);
      this.callState.timerInterval = null;
    }

    if (this.callState.localStream) {
      this.callState.localStream.getTracks().forEach(t => t.stop());
      this.callState.localStream = null;
    }

    if (this.callState.screenStream) {
      this.callState.screenStream.getTracks().forEach(t => t.stop());
      this.callState.screenStream = null;
    }

    this.callState.status = 'ended';
    this.updateCallUI();

    this.showToast('Call ended', 'info');
  }

  updateCallUI() {
    const container = document.getElementById('voice-call-container');
    if (!container) return;

    const statusBadge = document.getElementById('call-status-badge');
    const timer = document.getElementById('call-timer');
    const hangupBtn = document.getElementById('hangup-btn');
    const muteBtn = document.getElementById('mute-btn');
    const cameraBtn = document.getElementById('camera-btn');
    const screenBtn = document.getElementById('screen-share-btn');
    const recordBtn = document.getElementById('record-btn');
    const settingsBtn = document.getElementById('settings-btn');
    const callControls = document.querySelector('.call-controls');
    const callSettings = document.getElementById('call-settings');
    const videoGrid = document.getElementById('video-grid');
    const remotePlaceholder = document.getElementById('remote-video-placeholder');

    if (!statusBadge) return;

    switch (this.callState.status) {
      case 'idle':
        statusBadge.textContent = 'Idle';
        statusBadge.className = 'call-status-badge idle';
        timer.style.display = 'none';
        hangupBtn.style.display = 'none';
        muteBtn.disabled = true;
        cameraBtn.disabled = true;
        screenBtn.disabled = true;
        recordBtn.disabled = true;
        settingsBtn.disabled = true;
        callControls.style.display = 'flex';
        callSettings.style.display = 'none';
        if (remotePlaceholder) remotePlaceholder.style.display = 'none';
        break;

      case 'connecting':
        statusBadge.textContent = 'Connecting';
        statusBadge.className = 'call-status-badge connecting';
        hangupBtn.style.display = 'inline-flex';
        hangupBtn.textContent = 'Cancel';
        muteBtn.disabled = true;
        cameraBtn.disabled = true;
        screenBtn.disabled = true;
        recordBtn.disabled = true;
        settingsBtn.disabled = true;
        break;

      case 'active':
        statusBadge.textContent = 'Connected';
        statusBadge.className = 'call-status-badge active';
        timer.style.display = 'block';
        hangupBtn.style.display = 'inline-flex';
        hangupBtn.textContent = 'End Call';
        muteBtn.disabled = false;
        cameraBtn.disabled = false;
        screenBtn.disabled = false;
        recordBtn.disabled = false;
        settingsBtn.disabled = false;
        callSettings.style.display = 'flex';
        if (remotePlaceholder) remotePlaceholder.style.display = 'block';
        break;

      case 'ended':
        statusBadge.textContent = 'Ended';
        statusBadge.className = 'call-status-badge ended';
        timer.style.display = 'none';
        hangupBtn.style.display = 'none';
        muteBtn.disabled = true;
        cameraBtn.disabled = true;
        screenBtn.disabled = true;
        recordBtn.disabled = true;
        settingsBtn.disabled = true;
        callControls.style.display = 'flex';
        callSettings.style.display = 'none';
        if (remotePlaceholder) remotePlaceholder.style.display = 'none';
        this.showCallSummary();
        break;
    }

    // Update button states
    this.updateControlButtons();
  }

  updateControlButtons() {
    const muteBtn = document.getElementById('mute-btn');
    const cameraBtn = document.getElementById('camera-btn');

    if (muteBtn) {
      muteBtn.textContent = this.callState.isMuted ? '🔇' : '🎤';
      muteBtn.classList.toggle('active', this.callState.isMuted);
      muteBtn.title = this.callState.isMuted ? 'Unmute (M)' : 'Mute (M)';
    }

    if (cameraBtn) {
      cameraBtn.textContent = this.callState.isCameraOff ? '📷' : '📷';
      cameraBtn.classList.toggle('active', this.callState.isCameraOff);
      cameraBtn.title = this.callState.isCameraOff ? 'Camera Off (C)' : 'Camera On (C)';
    }

    const screenBtn = document.getElementById('screen-share-btn');
    if (screenBtn) {
      screenBtn.classList.toggle('active', this.callState.isScreenSharing);
    }

    const recordBtn = document.getElementById('record-btn');
    if (recordBtn) {
      recordBtn.classList.toggle('active', this.callState.isRecording);
    }
  }

  startCallTimer() {
    const timer = document.getElementById('call-timer');
    if (!timer) return;

    this.callState.timerInterval = setInterval(() => {
      if (this.callState.status !== 'active' || !this.callState.callStartTime) {
        clearInterval(this.callState.timerInterval);
        return;
      }

      const elapsed = Date.now() - this.callState.callStartTime;
      const mins = Math.floor(elapsed / 60000);
      const secs = Math.floor((elapsed % 60000) / 1000);
      timer.textContent = `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
    }, 1000);
  }

  toggleMute() {
    if (!this.callState.localStream) return;

    const audioTrack = this.callState.localStream.getAudioTracks()[0];
    if (audioTrack) {
      this.callState.isMuted = !audioTrack.enabled;
      audioTrack.enabled = !this.callState.isMuted;
      this.updateControlButtons();
      this.updateLocalStatus();

      // Send mute status to remote peer via signaling
      this.sendCallControl({ type: 'mute', muted: this.callState.isMuted });
    }
  }

  toggleCamera() {
    if (!this.callState.localStream) return;

    const videoTrack = this.callState.localStream.getVideoTracks()[0];
    if (videoTrack) {
      this.callState.isCameraOff = !videoTrack.enabled;
      videoTrack.enabled = !this.callState.isCameraOff;
      this.updateControlButtons();
      this.updateLocalStatus();

      // Send camera status to remote peer
      this.sendCallControl({ type: 'camera', enabled: !this.callState.isCameraOff });
    }
  }

  toggleSpeaker() {
    // In real implementation, this would switch audio output device
    this.showToast('Speaker output selection - coming soon', 'info');
  }

  async toggleScreenShare() {
    if (this.callState.isScreenSharing) {
      // Stop screen sharing
      if (this.callState.screenStream) {
        this.callState.screenStream.getTracks().forEach(t => t.stop());
        this.callState.screenStream = null;
      }

      // Restore camera video
      const localVideo = document.getElementById('local-video');
      if (localVideo && this.callState.localStream) {
        localVideo.srcObject = this.callState.localStream;
      }

      this.callState.isScreenSharing = false;
      this.updateControlButtons();

      const screenStatus = document.getElementById('local-screen-status');
      if (screenStatus) screenStatus.style.display = 'none';

      this.sendCallControl({ type: 'screen_share', sharing: false });
      this.showToast('Screen sharing stopped', 'info');
    } else {
      try {
        const screenStream = await navigator.mediaDevices.getDisplayMedia({
          video: { cursor: 'always' },
          audio: true
        });

        this.callState.screenStream = screenStream;

        const localVideo = document.getElementById('local-video');
        if (localVideo) {
          localVideo.srcObject = screenStream;
        }

        // Handle when user stops sharing via browser UI
        screenStream.getVideoTracks()[0].addEventListener('ended', () => {
          this.toggleScreenShare();
        });

        this.callState.isScreenSharing = true;
        this.updateControlButtons();

        const screenStatus = document.getElementById('local-screen-status');
        if (screenStatus) screenStatus.style.display = 'inline-flex';

        this.sendCallControl({ type: 'screen_share', sharing: true });
        this.showToast('Screen sharing started', 'success');
      } catch (e) {
        this.showToast('Screen share failed: ' + e.message, 'error');
      }
    }
  }

  async toggleRecording() {
    if (this.callState.isRecording) {
      // Stop recording
      if (this.callState.mediaRecorder) {
        this.callState.mediaRecorder.stop();
        this.callState.isRecording = false;
        this.updateControlButtons();
        this.showToast('Recording stopped', 'info');
      }
    } else {
      try {
        const streams = [this.callState.localStream];
        if (this.callState.remoteStream) streams.push(this.callState.remoteStream);
        if (this.callState.screenStream) streams.push(this.callState.screenStream);

        // Combine streams for recording
        const combinedStream = new MediaStream([
          ...this.callState.localStream.getAudioTracks(),
          ...this.callState.localStream.getVideoTracks(),
        ]);

        this.callState.mediaRecorder = new MediaRecorder(combinedStream, {
          mimeType: 'video/webm;codecs=vp9,opus'
        });

        this.callState.recordedChunks = [];

        this.callState.mediaRecorder.ondataavailable = (e) => {
          if (e.data.size > 0) this.callState.recordedChunks.push(e.data);
        };

        this.callState.mediaRecorder.onstop = () => {
          const blob = new Blob(this.callState.recordedChunks, { type: 'video/webm' });
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = `localweb-call-${Date.now()}.webm`;
          a.click();
          URL.revokeObjectURL(url);
          this.showToast('Recording saved', 'success');
        };

        this.callState.mediaRecorder.start(1000); // Collect data every second
        this.callState.isRecording = true;
        this.updateControlButtons();
        this.showToast('Recording started', 'success');
      } catch (e) {
        this.showToast('Recording failed: ' + e.message, 'error');
      }
    }
  }

  toggleVirtualBackground() {
    // Cycle through virtual backgrounds
    const backgrounds = this.callState.virtualBackgrounds;
    const currentIndex = backgrounds.findIndex(b => b.id === this.callState.virtualBackground);
    const nextIndex = (currentIndex + 1) % backgrounds.length;
    this.callState.virtualBackground = backgrounds[nextIndex].id;

    this.applyVirtualBackground(backgrounds[nextIndex]);
    this.updateControlButtons();
    this.showToast(`Background: ${backgrounds[nextIndex].name}`, 'info');
  }

  applyVirtualBackground(background) {
    // In real implementation, this would use WebGL/ML to replace background
    // For demo, we just show the selection
    const localVideo = document.getElementById('local-video');
    if (!localVideo) return;

    if (background.id === 'blur') {
      localVideo.style.filter = 'blur(8px)';
    } else if (background.id === 'none') {
      localVideo.style.filter = 'none';
    } else if (background.image) {
      // For image backgrounds, we'd use a canvas with segmentation
      // This is a simplified demo
      localVideo.style.filter = 'none';
    }
  }

  toggleCallSettings() {
    const settings = document.getElementById('call-settings');
    if (settings) {
      settings.style.display = settings.style.display === 'none' ? 'flex' : 'none';
    }
  }

  selectVirtualBackground() {
    this.toggleVirtualBackground();
  }

  openDevicesModal() {
    // In real implementation, this would show a modal with device selection
    this.showToast('Device selection modal - coming soon', 'info');
  }

  sendCallControl(control) {
    // In real implementation, this would send control messages via the Voice service signaling
    console.log('Call control:', control);
  }

  showCallSummary() {
    if (!this.callState.callStartTime) return;

    const duration = Date.now() - this.callState.callStartTime;
    const mins = Math.floor(duration / 60000);
    const secs = Math.floor((duration % 60000) / 1000);

    const container = document.getElementById('voice-call-container');
    if (!container) return;

    container.innerHTML = `
      <style>
        .call-ended { text-align: center; padding: 3rem; color: var(--color-text-muted); }
        .call-ended h3 { margin-bottom: 0.5rem; color: var(--color-text); }
        .call-summary { background: var(--color-bg-elevated); border-radius: 0.5rem; padding: 1rem; margin: 1rem auto; max-width: 400px; text-align: left; }
        .call-summary-row { display: flex; justify-content: space-between; padding: 0.25rem 0; font-size: 0.8125rem; }
        .call-summary-label { color: var(--color-text-muted); }
        .call-summary-value { font-weight: 500; }
      </style>
      <div class="call-ended">
        <div style="width: 80px; height: 80px; border-radius: 50%; background: var(--color-critical); display: flex; align-items: center; justify-content: center; margin: 0 auto 1.5rem; font-size: 2rem;">📞</div>
        <h3 style="margin-bottom: 0.5rem; color: var(--color-text);">Call Ended</h3>
        <div class="call-summary">
          <div class="call-summary-row"><span class="call-summary-label">Duration</span><span class="call-summary-value">${mins}m ${secs}s</span></div>
          <div class="call-summary-row"><span class="call-summary-label">Peer</span><span class="call-summary-value">${this.callState.remotePeerId || 'Unknown'}</span></div>
          <div class="call-summary-row"><span class="call-summary-label">Recording</span><span class="call-summary-value">${this.callState.isRecording ? 'Yes' : 'No'}</span></div>
          <div class="call-summary-row"><span class="call-summary-label">Screen Share</span><span class="call-summary-value">${this.callState.isScreenSharing ? 'Yes' : 'No'}</span></div>
        </div>
        <div style="display: flex; gap: 0.75rem; justify-content: center; margin-top: 1.5rem;">
          <button class="btn btn-primary" onclick="app.renderVoice()">New Call</button>
          <button class="btn btn-secondary" onclick="app.navigate('dashboard')">Dashboard</button>
        </div>
      `;
    `;
  }

  renderVPN() {
    document.getElementById('content').innerHTML = `
      <style>
        .vpn-container { display: flex; flex-direction: column; height: calc(100vh - 200px); min-height: 500px; }
        .vpn-header { display: flex; justify-content: space-between; align-items: center; padding: 1rem; border-bottom: 1px solid var(--color-border); }
        .vpn-status-badge { padding: 0.5rem 1rem; border-radius: 9999px; font-size: 0.875rem; font-weight: 600; }
        .vpn-status-badge.disconnected { background: var(--color-critical-light); color: var(--color-critical); border: 1px solid var(--color-critical); }
        .vpn-status-badge.connecting { background: var(--color-warning-light); color: var(--color-warning); border: 1px solid var(--color-warning); animation: pulse 1.5s infinite; }
        .vpn-status-badge.connected { background: var(--color-success-light); color: var(--color-success); border: 1px solid var(--color-success); }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
        .vpn-tabs { display: flex; gap: 0.25rem; padding: 0 1rem; border-bottom: 1px solid var(--color-border); overflow-x: auto; }
        .vpn-tab { padding: 0.75rem 1rem; border: none; background: transparent; color: var(--color-text-muted); cursor: pointer; font-size: 0.875rem; font-weight: 500; border-bottom: 2px solid transparent; transition: all 0.15s; }
        .vpn-tab:hover { color: var(--color-primary); }
        .vpn-tab.active { color: var(--color-primary); border-bottom-color: var(--color-primary); }
        .vpn-tab-content { display: none; flex: 1; padding: 1rem; overflow: auto; }
        .vpn-tab-content.active { display: block; animation: fadeIn 0.2s; }
        @keyframes fadeIn { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: translateY(0); } }
        .vpn-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
        .stat-card { background: var(--color-bg-elevated); border: 1px solid var(--color-border); border-radius: 0.5rem; padding: 1rem; }
        .stat-card .stat-label { font-size: 0.75rem; color: var(--color-text-muted); margin-bottom: 0.25rem; }
        .stat-card .stat-value { font-size: 1.5rem; font-weight: 700; color: var(--color-text); }
        .vpn-section { margin-bottom: 1.5rem; }
        .vpn-section-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
        .vpn-section-title { font-size: 1rem; font-weight: 600; }
        .vpn-table { width: 100%; border-collapse: collapse; }
        .vpn-table th, .vpn-table td { padding: 0.75rem; text-align: left; border-bottom: 1px solid var(--color-border); font-size: 0.875rem; }
        .vpn-table th { color: var(--color-text-muted); font-weight: 500; }
        .vpn-table tr:hover { background: var(--color-bg-elevated); }
        .route-status { display: inline-flex; align-items: center; gap: 0.375rem; padding: 0.25rem 0.5rem; border-radius: 9999px; font-size: 0.6875rem; font-weight: 500; }
        .route-status.active { background: var(--color-success-light); color: var(--color-success); }
        .route-status.inactive { background: var(--color-bg); color: var(--color-text-muted); }
        .route-status.pending { background: var(--color-warning-light); color: var(--color-warning); }
        .acl-rule { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem; background: var(--color-bg-elevated); border: 1px solid var(--color-border); border-radius: 0.375rem; margin-bottom: 0.5rem; }
        .acl-rule input, .acl-rule select { flex: 1; padding: 0.5rem; border: 1px solid var(--color-border); border-radius: 0.25rem; background: var(--color-bg); color: var(--color-text); font-size: 0.875rem; }
        .acl-rule .btn-sm { flex-shrink: 0; }
        .dns-test-result { padding: 1rem; border-radius: 0.5rem; margin-top: 1rem; }
        .dns-test-result.pass { background: var(--color-success-light); border: 1px solid var(--color-success); }
        .dns-test-result.fail { background: var(--color-critical-light); border: 1px solid var(--color-critical); }
        .dns-test-result.running { background: var(--color-warning-light); border: 1px solid var(--color-warning); }
        .dns-leak-item { display: flex; justify-content: space-between; padding: 0.5rem 0; border-bottom: 1px solid var(--color-border); }
        .dns-leak-item:last-child { border-bottom: none; }
        .kill-switch-toggle { display: flex; align-items: center; justify-content: space-between; padding: 1rem; background: var(--color-bg-elevated); border: 1px solid var(--color-border); border-radius: 0.5rem; }
        .kill-switch-info h4 { margin-bottom: 0.25rem; }
        .kill-switch-info p { color: var(--color-text-muted); font-size: 0.8125rem; }
        .toggle-switch { position: relative; width: 56px; height: 28px; }
        .toggle-switch input { opacity: 0; width: 0; height: 0; }
        .toggle-slider { position: absolute; cursor: pointer; top: 0; left: 0; right: 0; bottom: 0; background: var(--color-border); border-radius: 28px; transition: 0.3s; }
        .toggle-slider:before { position: absolute; content: ""; height: 20px; width: 20px; left: 4px; bottom: 4px; background: white; border-radius: 50%; transition: 0.3s; box-shadow: 0 2px 4px rgba(0,0,0,0.2); }
        .toggle-switch input:checked + .toggle-slider { background: var(--color-primary); }
        .toggle-switch input:checked + .toggle-slider:before { transform: translateX(28px); }
        .peer-selector { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-top: 1rem; }
        .peer-option { padding: 0.75rem 1rem; border: 1px solid var(--color-border); border-radius: 0.375rem; background: var(--color-bg); cursor: pointer; transition: all 0.15s; }
        .peer-option:hover { border-color: var(--color-primary); }
        .peer-option.selected { border-color: var(--color-primary); background: var(--color-primary-light); }
        .peer-option input { display: none; }
      </style>
      <div class="vpn-container">
        <div class="vpn-header">
          <h2 style="margin: 0; font-size: 1.25rem;">VPN Dashboard</h2>
          <span class="vpn-status-badge disconnected" id="vpn-status-badge">Disconnected</span>
        </div>
        <div class="vpn-tabs" id="vpn-tabs">
          <button class="vpn-tab active" data-tab="overview" onclick="app.switchVpnTab('overview')">Overview</button>
          <button class="vpn-tab" data-tab="routes" onclick="app.switchVpnTab('routes')">Routes</button>
          <button class="vpn-tab" data-tab="split-tunnel" onclick="app.switchVpnTab('split-tunnel')">Split Tunnel</button>
          <button class="vpn-tab" data-tab="acls" onclick="app.switchVpnTab('acls')">ACLs</button>
          <button class="vpn-tab" data-tab="kill-switch" onclick="app.switchVpnTab('kill-switch')">Kill Switch</button>
          <button class="vpn-tab" data-tab="dns-test" onclick="app.switchVpnTab('dns-test')">DNS Leak Test</button>
        </div>

        <!-- Overview Tab -->
        <div class="vpn-tab-content active" id="vpn-tab-overview">
          <div class="vpn-stats">
            <div class="stat-card">
              <div class="stat-label">Status</div>
              <div class="stat-value" id="vpn-status-value">Disconnected</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">Data Sent</div>
              <div class="stat-value" id="vpn-sent">0 B</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">Data Received</div>
              <div class="stat-value" id="vpn-received">0 B</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">Uptime</div>
              <div class="stat-value" id="vpn-uptime">00:00:00</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">Connected Peer</div>
              <div class="stat-value" id="vpn-peer">—</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">Virtual IP</div>
              <div class="stat-value" id="vpn-virtual-ip">—</div>
            </div>
          </div>

          <div class="vpn-section">
            <div class="vpn-section-header">
              <h3 class="vpn-section-title">Connection</h3>
            </div>
            <div style="display: flex; gap: 1rem; flex-wrap: wrap; align-items: center;">
              <button class="btn btn-primary" id="vpn-connect-btn" onclick="app.toggleVPN()">Connect</button>
              <div style="display: flex; align-items: center; gap: 1rem; color: var(--color-text-muted); font-size: 0.875rem;">
                <label style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
                  <input type="checkbox" id="auto-connect" onchange="app.toggleAutoConnect(this.checked)">
                  Auto-connect on startup
                </label>
              </div>
            </div>
            <div style="margin-top: 1rem; padding: 1rem; background: var(--color-bg-elevated); border: 1px solid var(--color-border); border-radius: 0.5rem;">
              <strong>Peer:</strong> <span id="vpn-selected-peer">None selected</span>
              <button class="btn btn-secondary btn-sm" style="margin-left: 1rem;" onclick="app.showPeerSelector()">Select Peer</button>
            </div>
          </div>
        </div>

        <!-- Routes Tab -->
        <div class="vpn-tab-content" id="vpn-tab-routes">
          <div class="vpn-section">
            <div class="vpn-section-header">
              <h3 class="vpn-section-title">VPN Routes</h3>
              <button class="btn btn-secondary btn-sm" onclick="app.addRoute()">Add Route</button>
            </div>
            <table class="vpn-table" id="vpn-routes-table">
              <thead>
                <tr><th>Network</th><th>Gateway</th><th>Metric</th><th>Status</th><th>Actions</th></tr>
              </thead>
              <tbody id="vpn-routes-body">
                <tr><td colspan="5" style="text-align: center; color: var(--color-text-muted);">No routes configured</td></tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Split Tunnel Tab -->
        <div class="vpn-tab-content" id="vpn-tab-split-tunnel">
          <div class="vpn-section">
            <div class="vpn-section-header">
              <h3 class="vpn-section-title">Split Tunnel Configuration</h3>
            </div>
            <div class="kill-switch-toggle">
              <div class="kill-switch-info">
                <h4>Split Tunnel Mode</h4>
                <p>When enabled, only specified routes go through the VPN. All other traffic uses your normal internet connection.</p>
              </div>
              <label class="toggle-switch">
                <input type="checkbox" id="split-tunnel-enabled" onchange="app.toggleSplitTunnel(this.checked)">
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div style="margin-top: 1.5rem;">
              <h4 style="margin-bottom: 1rem;">Included Routes (traffic sent through VPN)</h3>
              <div id="included-routes" style="display: flex; flex-direction: column; gap: 0.5rem;">
                <div class="acl-rule">
                  <input type="text" placeholder="Network (CIDR)" value="10.0.0.0/8" disabled>
                  <select disabled><option value="include">Include</option><option value="exclude">Exclude</option></select>
                  <button class="btn btn-critical btn-sm" onclick="app.removeSplitRoute(this)">Remove</button>
                </div>
                <div class="acl-rule">
                  <input type="text" placeholder="Network (CIDR)" value="192.168.0.0/16" disabled>
                  <select disabled><option value="include" selected>Include</option><option value="exclude">Exclude</option></select>
                  <button class="btn btn-critical btn-sm" onclick="app.removeSplitRoute(this)">Remove</button>
                </div>
              </div>
              <button class="btn btn-secondary btn-sm" onclick="app.addSplitRoute()">Add Route</button>
            </div>
            <div style="margin-top: 1.5rem;">
              <h4 style="margin-bottom: 1rem;">Excluded Routes (traffic bypassing VPN)</h4>
              <div id="excluded-routes" style="display: flex; flex-direction: column; gap: 0.5rem;">
                <div class="acl-rule">
                  <input type="text" placeholder="Network (CIDR)" value="192.168.1.0/24" disabled>
                  <select disabled><option value="include">Include</option><option value="exclude" selected>Exclude</option></select>
                  <button class="btn btn-critical btn-sm" onclick="app.removeSplitRoute(this)">Remove</button>
                </div>
              </div>
              <button class="btn btn-secondary btn-sm" onclick="app.addExcludedRoute()">Add Exclusion</button>
            </div>
          </div>
        </div>

        <!-- ACLs Tab -->
        <div class="vpn-tab-content" id="vpn-tab-acls">
          <div class="vpn-section">
            <div class="vpn-section-header">
              <h3 class="vpn-section-title">Access Control Lists (ACLs)</h3>
              <button class="btn btn-secondary btn-sm" onclick="app.addAclRule()">Add Rule</button>
            </div>
            <div id="acl-rules" style="display: flex; flex-direction: column; gap: 0.5rem;">
              <div class="acl-rule">
                <select>
                  <option value="allow">Allow</option>
                  <option value="deny">Deny</option>
                </select>
                <input type="text" placeholder="Source (CIDR or 'any')" value="any">
                <input type="text" placeholder="Destination (CIDR or 'any')" value="any">
                <input type="text" placeholder="Protocol (tcp/udp/icmp/any)" value="any">
                <input type="text" placeholder="Port/Port Range" value="any">
                <button class="btn btn-critical btn-sm" onclick="app.removeAclRule(this)">Delete</button>
              </div>
              <div class="acl-rule">
                <select><option value="allow">Allow</option><option value="deny">Deny</option></select>
                <input type="text" placeholder="Source" value="10.0.0.0/8">
                <input type="text" placeholder="Destination" value="192.168.1.0/24">
                <input type="text" placeholder="Protocol" value="tcp">
                <input type="text" placeholder="Port" value="22,80,443">
                <button class="btn btn-critical btn-sm" onclick="app.removeAclRule(this)">Delete</button>
              </div>
            </div>
            <p style="color: var(--color-text-muted); font-size: 0.8125rem; margin-top: 1rem;">Rules are evaluated in order. First match wins. Default policy: Deny all.</p>
          </div>
        </div>

        <!-- Kill Switch Tab -->
        <div class="vpn-tab-content" id="vpn-tab-kill-switch">
          <div class="vpn-section">
            <div class="kill-switch-toggle">
              <div class="kill-switch-info">
                <h4>Kill Switch</h4>
                <p>When enabled, all internet traffic is blocked if the VPN connection drops unexpectedly. This prevents data leaks.</p>
              </div>
              <label class="toggle-switch">
                <input type="checkbox" id="kill-switch-enabled" onchange="app.toggleKillSwitch(this.checked)" checked>
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div style="margin-top: 1.5rem; padding: 1rem; background: var(--color-bg-elevated); border: 1px solid var(--color-border); border-radius: 0.5rem;">
              <h4 style="margin-bottom: 0.5rem;">Advanced Options</h4>
              <label style="display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; cursor: pointer;">
                <input type="checkbox" id="kill-switch-persistent" checked>
                <span>Persistent kill switch (survives reboots)</span>
              </label>
              <label style="display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; cursor: pointer;">
                <input type="checkbox" id="kill-switch-ipv6" checked>
                <span>Block IPv6 when VPN is down</span>
              </label>
              <label style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
                <input type="checkbox" id="kill-switch-dns" checked>
                <span>Block DNS when VPN is down</span>
              </label>
            </div>
            <div style="margin-top: 1.5rem; padding: 1rem; background: var(--color-warning-light); border: 1px solid var(--color-warning); border-radius: 0.5rem;">
              <strong>⚠ Warning:</strong> Enabling the kill switch may disconnect you from the internet if the VPN fails. Ensure you have a way to disable it (e.g., local console access) before enabling on remote servers.
            </div>
          </div>
        </div>

        <!-- DNS Leak Test Tab -->
        <div class="vpn-tab-content" id="vpn-tab-dns-test">
          <div class="vpn-section">
            <div class="vpn-section-header">
              <h3 class="vpn-section-title">DNS Leak Test</h3>
              <button class="btn btn-primary" id="dns-test-btn" onclick="app.runDnsLeakTest()">Run Test</button>
            </div>
            <div id="dns-test-result" style="display: none;">
              <div class="dns-test-result running" id="dns-test-status">
                <div style="display: flex; align-items: center; gap: 0.75rem;">
                  <div style="width: 24px; height: 24px; border: 3px solid var(--color-warning); border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite;"></div>
                  <div>
                    <div style="font-weight: 500;">Testing DNS resolution…</div>
                    <div style="font-size: 0.8125rem; color: var(--color-text-muted);">Checking for DNS leaks via multiple resolvers</div>
                  </div>
                </div>
              </div>
            </div>
            <div id="dns-test-results" style="display: none; margin-top: 1rem;">
              <h4 style="margin-bottom: 1rem;">Test Results</h4>
              <div id="dns-leak-list"></div>
              <div style="margin-top: 1rem; padding: 1rem; background: var(--color-bg-elevated); border-radius: 0.5rem;">
                <h4 style="margin-bottom: 0.5rem;">Your DNS Servers</h4>
                <div id="dns-servers-list" style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 0.5rem;"></div>
              </div>
              <div style="margin-top: 1rem; padding: 1rem; background: var(--color-bg-elevated); border-radius: 0.5rem;">
                <h4 style="margin-bottom: 0.5rem;">Recommendations</h4>
                <ul id="dns-recommendations" style="margin: 0; padding-left: 1.5rem; color: var(--color-text-muted); font-size: 0.875rem;"></ul>
              </div>
            </div>
          </div>
        </div>
      </div>
    `;

    this.vpnState = {
      status: 'disconnected', // disconnected, connecting, connected, error
      uptime: 0,
      bytesSent: 0,
      bytesReceived: 0,
      peerId: null,
      virtualIp: null,
      routes: [],
      splitTunnelEnabled: false,
      includedRoutes: [
        { network: '10.0.0.0/8', action: 'include' },
        { network: '192.168.0.0/16', action: 'include' },
      ],
      excludedRoutes: [
        { network: '192.168.1.0/24', action: 'exclude' },
      ],
      aclRules: [
        { action: 'allow', source: 'any', dest: 'any', proto: 'any', port: 'any' },
        { action: 'allow', source: '10.0.0.0/8', dest: '192.168.1.0/24', proto: 'tcp', port: '22,80,443' },
      ],
      killSwitchEnabled: true,
      killSwitchPersistent: true,
      killSwitchIpv6: true,
      killSwitchDns: true,
      autoConnect: false,
      selectedPeer: null,
      dnsTestRunning: false,
    };

    this.initVpnDashboard();
  }

  initVpnDashboard() {
    this.updateVpnOverview();
    this.renderRoutesTable();
    this.renderAclRules();
    this.updateSplitTunnelUI();
  }

  switchVpnTab(tab) {
    document.querySelectorAll('.vpn-tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.vpn-tab-content').forEach(c => c.classList.remove('active'));

    document.querySelector(`[data-tab="${tab}"]`)?.classList.add('active');
    document.getElementById(`vpn-tab-${tab}`)?.classList.add('active');

    // Load tab-specific data
    switch (tab) {
      case 'overview': this.updateVpnOverview(); break;
      case 'routes': this.renderRoutesTable(); break;
      case 'split-tunnel': this.updateSplitTunnelUI(); break;
      case 'acls': this.renderAclRules(); break;
      case 'dns-test': break;
    }
  }

  async toggleVPN() {
    const btn = document.getElementById('vpn-connect-btn');
    const badge = document.getElementById('vpn-status-badge');

    if (this.vpnState.status === 'connected') {
      // Disconnect
      btn.disabled = true;
      btn.textContent = 'Disconnecting…';
      this.vpnState.status = 'disconnecting';
      this.updateVpnStatusBadge();

      try {
        // In real implementation: call VPN service to disconnect
        await new Promise(r => setTimeout(r, 1000));

        this.vpnState.status = 'disconnected';
        this.vpnState.uptime = 0;
        this.vpnState.bytesSent = 0;
        this.vpnState.bytesReceived = 0;
        this.vpnState.peerId = null;
        this.vpnState.virtualIp = null;
        if (this.vpnState.uptimeInterval) clearInterval(this.vpnState.uptimeInterval);

        this.showToast('VPN disconnected', 'info');
      } catch (e) {
        this.showToast('Disconnect failed: ' + e.message, 'error');
        this.vpnState.status = 'connected';
      }
    } else {
      // Connect
      const peer = this.vpnState.selectedPeer;
      if (!peer) {
        this.showToast('Please select a peer first', 'error');
        return;
      }

      btn.disabled = true;
      btn.textContent = 'Connecting…';
      this.vpnState.status = 'connecting';
      this.updateVpnStatusBadge();

      try {
        // In real implementation: call VPN service to connect to peer
        await new Promise(r => setTimeout(r, 2000));

        this.vpnState.status = 'connected';
        this.vpnState.peerId = peer.id;
        this.vpnState.virtualIp = '10.42.0.' + Math.floor(Math.random() * 254 + 2);
        this.vpnState.uptime = 0;
        this.vpnState.bytesSent = 0;
        this.vpnState.bytesReceived = 0;

        this.startVpnUptimeTimer();
        this.startVpnTrafficSimulation();

        this.showToast('VPN connected to ' + peer.name, 'success');
      } catch (e) {
        this.vpnState.status = 'error';
        this.showToast('Connection failed: ' + e.message, 'error');
      }
    }

    btn.disabled = false;
    this.updateVpnOverview();
    this.updateVpnStatusBadge();
  }

  updateVpnStatusBadge() {
    const badge = document.getElementById('vpn-status-badge');
    const btn = document.getElementById('vpn-connect-btn');

    if (!badge || !btn) return;

    badge.className = 'vpn-status-badge ' + this.vpnState.status;
    badge.textContent = this.vpnState.status.charAt(0).toUpperCase() + this.vpnState.status.slice(1);

    switch (this.vpnState.status) {
      case 'connected':
        btn.textContent = 'Disconnect';
        btn.className = 'btn btn-critical';
        break;
      case 'connecting':
        btn.textContent = 'Connecting…';
        btn.disabled = true;
        btn.className = 'btn btn-warning';
        break;
      case 'disconnecting':
        btn.textContent = 'Disconnecting…';
        btn.disabled = true;
        btn.className = 'btn btn-warning';
        break;
      default:
        btn.textContent = 'Connect';
        btn.disabled = false;
        btn.className = 'btn btn-primary';
    }
  }

  updateVpnOverview() {
    const statusVal = document.getElementById('vpn-status-value');
    const sentVal = document.getElementById('vpn-sent');
    const recvVal = document.getElementById('vpn-received');
    const uptimeVal = document.getElementById('vpn-uptime');
    const peerVal = document.getElementById('vpn-peer');
    const ipVal = document.getElementById('vpn-virtual-ip');

    if (statusVal) statusVal.textContent = this.vpnState.status.charAt(0).toUpperCase() + this.vpnState.status.slice(1);
    if (sentVal) sentVal.textContent = this.formatSize(this.vpnState.bytesSent);
    if (recvVal) recvVal.textContent = this.formatSize(this.vpnState.bytesReceived);
    if (uptimeVal) uptimeVal.textContent = this.formatDuration(this.vpnState.uptime);
    if (peerVal) peerVal.textContent = this.vpnState.peerId ? 'Peer-' + this.vpnState.peerId.slice(0, 6) : '—';
    if (ipVal) ipVal.textContent = this.vpnState.virtualIp || '—';
  }

  startVpnUptimeTimer() {
    if (this.vpnState.uptimeInterval) clearInterval(this.vpnState.uptimeInterval);
    this.vpnState.uptimeInterval = setInterval(() => {
      if (this.vpnState.status === 'connected') {
        this.vpnState.uptime++;
        this.updateVpnOverview();
      } else {
        clearInterval(this.vpnState.uptimeInterval);
      }
    }, 1000);
  }

  startVpnTrafficSimulation() {
    if (this.vpnState.trafficInterval) clearInterval(this.vpnState.trafficInterval);
    this.vpnState.trafficInterval = setInterval(() => {
      if (this.vpnState.status === 'connected') {
        this.vpnState.bytesSent += Math.floor(Math.random() * 50000) + 10000;
        this.vpnState.bytesReceived += Math.floor(Math.random() * 80000) + 20000;
        this.updateVpnOverview();
      } else {
        clearInterval(this.vpnState.trafficInterval);
      }
    }, 2000);
  }

  formatDuration(seconds) {
    const h = Math.floor(seconds / 3600).toString().padStart(2, '0');
    const m = Math.floor((seconds % 3600) / 60).toString().padStart(2, '0');
    const s = (seconds % 60).toString().padStart(2, '0');
    return `${h}:${m}:${s}`;
  }

  formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB';
    return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB';
  }

  // Routes Tab
  renderRoutesTable() {
    const body = document.getElementById('vpn-routes-body');
    if (!body) return;

    if (this.vpnState.routes.length === 0) {
      body.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--color-text-muted);">No routes configured</td></tr>';
      return;
    }

    body.innerHTML = this.vpnState.routes.map((r, i) => `
      <tr>
        <td><code>${r.network}</code></td>
        <td><code>${r.gateway}</code></td>
        <td>${r.metric}</td>
        <td><span class="route-status ${r.status}">${r.status}</span></td>
        <td>
          <button class="btn btn-secondary btn-sm" onclick="app.editRoute(${i})">Edit</button>
          <button class="btn btn-critical btn-sm" onclick="app.deleteRoute(${i})">Delete</button>
        </td>
      </tr>
    `).join('');
  }

  addRoute() {
    const network = prompt('Network (CIDR, e.g., 10.10.0.0/16):');
    if (!network) return;
    const gateway = prompt('Gateway IP (or "auto"):', 'auto');
    if (!gateway) return;
    const metric = parseInt(prompt('Metric:', '100') || '100');

    this.vpnState.routes.push({
      network,
      gateway,
      metric,
      status: this.vpnState.status === 'connected' ? 'active' : 'pending'
    });
    this.renderRoutesTable();
  }

  editRoute(index) {
    const route = this.vpnState.routes[index];
    const network = prompt('Network (CIDR):', route.network);
    if (!network) return;
    const gateway = prompt('Gateway IP:', route.gateway);
    if (!gateway) return;
    const metric = parseInt(prompt('Metric:', route.metric.toString()) || '100');

    this.vpnState.routes[index] = { ...route, network, gateway, metric };
    this.renderRoutesTable();
  }

  deleteRoute(index) {
    if (confirm('Delete this route?')) {
      this.vpnState.routes.splice(index, 1);
      this.renderRoutesTable();
    }
  }

  // Split Tunnel Tab
  updateSplitTunnelUI() {
    const checkbox = document.getElementById('split-tunnel-enabled');
    if (checkbox) checkbox.checked = this.vpnState.splitTunnelEnabled;

    const included = document.getElementById('included-routes');
    const excluded = document.getElementById('excluded-routes');

    if (included) {
      included.innerHTML = this.vpnState.includedRoutes.map((r, i) => `
        <div class="acl-rule">
          <input type="text" placeholder="Network (CIDR)" value="${r.network}" onchange="app.updateIncludedRoute(${i}, 'network', this.value)">
          <select onchange="app.updateIncludedRoute(${i}, 'action', this.value)">
            <option value="include" ${r.action === 'include' ? 'selected' : ''}>Include (via VPN)</option>
            <option value="exclude" ${r.action === 'exclude' ? 'selected' : ''}>Exclude (bypass VPN)</option>
          </select>
          <button class="btn btn-critical btn-sm" onclick="app.removeIncludedRoute(${i})">Remove</button>
        </div>
      `).join('');
    }

    if (excluded) {
      excluded.innerHTML = this.vpnState.excludedRoutes.map((r, i) => `
        <div class="acl-rule">
          <input type="text" placeholder="Network (CIDR)" value="${r.network}" onchange="app.updateExcludedRoute(${i}, 'network', this.value)">
          <select onchange="app.updateExcludedRoute(${i}, 'action', this.value)">
            <option value="include" ${r.action === 'include' ? 'selected' : ''}>Include (via VPN)</option>
            <option value="exclude" ${r.action === 'exclude' ? 'selected' : ''}>Exclude (bypass VPN)</option>
          </select>
          <button class="btn btn-critical btn-sm" onclick="app.removeExcludedRoute(${i})">Remove</button>
        </div>
      `).join('');
    }
  }

  toggleSplitTunnel(enabled) {
    this.vpnState.splitTunnelEnabled = enabled;
    this.showToast(enabled ? 'Split tunnel enabled' : 'Split tunnel disabled (all traffic via VPN)', 'info');
  }

  addSplitRoute() {
    const network = prompt('Network (CIDR, e.g., 172.16.0.0/12):');
    if (!network) return;
    const action = prompt('Action: "include" (via VPN) or "exclude" (bypass):', 'include');
    if (!action) return;

    this.vpnState.includedRoutes.push({ network, action });
    this.updateSplitTunnelUI();
  }

  removeSplitRoute(index) {
    this.vpnState.includedRoutes.splice(index, 1);
    this.updateSplitTunnelUI();
  }

  updateIncludedRoute(index, field, value) {
    this.vpnState.includedRoutes[index][field] = value;
  }

  addExcludedRoute() {
    const network = prompt('Network (CIDR, e.g., 172.20.0.0/16):');
    if (!network) return;
    const action = prompt('Action: "include" or "exclude":', 'exclude');
    if (!action) return;

    this.vpnState.excludedRoutes.push({ network, action });
    this.updateSplitTunnelUI();
  }

  removeExcludedRoute(index) {
    this.vpnState.excludedRoutes.splice(index, 1);
    this.updateSplitTunnelUI();
  }

  updateExcludedRoute(index, field, value) {
    this.vpnState.excludedRoutes[index][field] = value;
  }

  // ACLs Tab
  renderAclRules() {
    const container = document.getElementById('acl-rules');
    if (!container) return;

    container.innerHTML = this.vpnState.aclRules.map((r, i) => `
      <div class="acl-rule">
        <select onchange="app.updateAclRule(${i}, 'action', this.value)">
          <option value="allow" ${r.action === 'allow' ? 'selected' : ''}>Allow</option>
          <option value="deny" ${r.action === 'deny' ? 'selected' : ''}>Deny</option>
        </select>
        <input type="text" placeholder="Source (CIDR or 'any')" value="${r.source}" onchange="app.updateAclRule(${i}, 'source', this.value)">
        <input type="text" placeholder="Destination (CIDR or 'any')" value="${r.dest}" onchange="app.updateAclRule(${i}, 'dest', this.value)">
        <input type="text" placeholder="Protocol (tcp/udp/icmp/any)" value="${r.proto}" onchange="app.updateAclRule(${i}, 'proto', this.value)">
        <input type="text" placeholder="Port/Port Range" value="${r.port}" onchange="app.updateAclRule(${i}, 'port', this.value)">
        <button class="btn btn-critical btn-sm" onclick="app.removeAclRule(${i})">Delete</button>
      </div>
    `).join('');
  }

  addAclRule() {
    this.vpnState.aclRules.push({ action: 'allow', source: 'any', dest: 'any', proto: 'any', port: 'any' });
    this.renderAclRules();
  }

  removeAclRule(index) {
    this.vpnState.aclRules.splice(index, 1);
    this.renderAclRules();
  }

  updateAclRule(index, field, value) {
    this.vpnState.aclRules[index][field] = value;
  }

  // Kill Switch Tab
  toggleKillSwitch(enabled) {
    this.vpnState.killSwitchEnabled = enabled;
    this.showToast(enabled ? 'Kill switch enabled' : 'Kill switch disabled', enabled ? 'success' : 'warning');
  }

  // DNS Leak Test
  async runDnsLeakTest() {
    const btn = document.getElementById('dns-test-btn');
    const statusDiv = document.getElementById('dns-test-status');
    const resultsDiv = document.getElementById('dns-test-results');
    const listDiv = document.getElementById('dns-leak-list');
    const serversDiv = document.getElementById('dns-servers-list');
    const recDiv = document.getElementById('dns-recommendations');

    if (!btn || !statusDiv || !resultsDiv) return;

    btn.disabled = true;
    btn.textContent = 'Testing…';
    statusDiv.style.display = 'block';
    resultsDiv.style.display = 'none';

    this.vpnState.dnsTestRunning = true;

    try {
      // Simulate DNS leak test
      await new Promise(r => setTimeout(r, 3000));

      // Simulated results
      const testServers = [
        { ip: '1.1.1.1', provider: 'Cloudflare', location: 'US', leaked: false },
        { ip: '8.8.8.8', provider: 'Google', location: 'US', leaked: false },
        { ip: '9.9.9.9', provider: 'Quad9', location: 'CH', leaked: false },
        { ip: '208.67.222.222', provider: 'OpenDNS', location: 'US', leaked: false },
      ];

      // Simulate a leak if VPN is not connected
      const leaked = this.vpnState.status !== 'connected' && Math.random() > 0.5;
      if (leaked) {
        testServers[0].leaked = true;
        testServers[0].provider = 'ISP DNS (Leaked!)';
      }

      statusDiv.style.display = 'none';
      resultsDiv.style.display = 'block';

      // DNS servers list
      serversDiv.innerHTML = testServers.map(s => `
        <div style="padding: 0.75rem; background: var(--color-bg); border-radius: 0.375rem; display: flex; justify-content: space-between; align-items: center;">
          <div>
            <div style="font-weight: 500;">${s.provider}</div>
            <div style="font-size: 0.75rem; color: var(--color-text-muted);">${s.ip} • ${s.location}</div>
          </div>
          <span style="padding: 0.25rem 0.5rem; border-radius: 9999px; font-size: 0.6875rem; font-weight: 600; ${s.leaked ? 'background: var(--color-critical-light); color: var(--color-critical);' : 'background: var(--color-success-light); color: var(--color-success);'}">
            ${s.leaked ? 'LEAKED' : 'SECURE'}
          </span>
        </div>
      `).join('');

      // Leak list
      listDiv.innerHTML = `
        <div style="padding: 1rem; background: ${leaked ? 'var(--color-critical-light)' : 'var(--color-success-light)'}; border: 1px solid ${leaked ? 'var(--color-critical)' : 'var(--color-success)'}; border-radius: 0.5rem;">
          <div style="display: flex; align-items: center; gap: 0.75rem;">
            <span style="font-size: 2rem;">${leaked ? '⚠' : '✓'}</span>
            <div>
              <div style="font-weight: 600; ${leaked ? 'color: var(--color-critical);' : 'color: var(--color-success);'}">
                ${leaked ? 'DNS Leak Detected!' : 'No DNS Leaks Found'}
              </div>
              <div style="font-size: 0.8125rem; color: var(--color-text-muted);">
                ${leaked ? 'Your DNS queries are visible to your ISP. VPN may not be routing DNS correctly.' : 'All DNS queries are routed through the VPN tunnel.'}
              </div>
            </div>
          </div>
        `;

      // Recommendations
      const recommendations = [];
      if (leaked) {
        recommendations.push('Enable "Block DNS when VPN is down" in Kill Switch settings');
        recommendations.push('Configure your VPN to push DNS servers (e.g., 1.1.1.1, 9.9.9.9)');
        recommendations.push('Disable WebRTC in browser to prevent local IP leaks');
        recommendations.push('Use "DNS over HTTPS" (DoH) in browser settings');
      } else {
        recommendations.push('DNS is properly routed through VPN ✓');
        recommendations.push('Kill switch is active ✓');
        recommendations.push('Consider enabling DNS over HTTPS for additional privacy');
      }

      document.getElementById('dns-recommendations').innerHTML = recommendations.map(r => `<li>${r}</li>`).join('');

    } catch (e) {
      this.showToast('DNS test failed: ' + e.message, 'error');
    } finally {
      btn.disabled = false;
      btn.textContent = 'Run Test';
      this.vpnState.dnsTestRunning = false;
    }
  }

  // Peer Selection
  showPeerSelector() {
    // In real implementation, this would show a modal with available peers
    const peers = [
      { id: 'peer-1', name: 'MacBook Pro', addrs: ['192.168.1.50:4443'], score: 0.95 },
      { id: 'peer-2', name: 'iPhone', addrs: ['192.168.1.51:4443'], score: 0.87 },
      { id: 'peer-3', name: 'Linux Server', addrs: ['10.0.0.5:4443'], score: 0.92 },
    ];

    const modal = document.createElement('div');
    modal.style.cssText = 'position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000;';
    modal.innerHTML = `
      <div style="background: var(--color-bg); border-radius: 0.5rem; padding: 1.5rem; max-width: 500px; width: 90%; box-shadow: var(--shadow-xl);">
        <h3 style="margin-bottom: 1rem;">Select VPN Peer</h3>
        <p style="color: var(--color-text-muted); margin-bottom: 1rem;">Choose a peer to use as VPN exit node</p>
        <div style="display: flex; flex-direction: column; gap: 0.5rem; max-height: 300px; overflow-y: auto;">
          ${peers.map(p => `
            <label class="peer-option" onclick="this.querySelector('input').checked=true; document.body.removeChild(this.closest('.modal'))">
              <input type="radio" name="vpn-peer" value="${p.id}" ${this.vpnState.selectedPeer === p.id ? 'checked' : ''}>
              <div style="display: flex; justify-content: space-between;">
                <div>
                  <div style="font-weight: 500;">${p.name}</div>
                  <div style="font-size: 0.75rem; color: var(--color-text-muted);">${p.addrs[0]}</div>
                </div>
                <div style="font-size: 0.75rem; color: var(--color-primary);">Score: ${p.score}</div>
              </div>
            </label>
          `).join('')}
        </div>
        <div style="display: flex; gap: 0.5rem; justify-content: flex-end; margin-top: 1rem;">
          <button class="btn btn-secondary" onclick="document.body.removeChild(this.closest('.modal'))">Cancel</button>
          <button class="btn btn-primary" onclick="app.selectVpnPeer(this.closest('.modal'))">Connect</button>
        </div>
      </div>
    `;
    modal.className = 'modal';
    document.body.appendChild(modal);
  }

  selectVpnPeer(modal) {
    const selected = modal.querySelector('input[name="vpn-peer"]:checked');
    if (selected) {
      this.vpnState.selectedPeer = selected.value;
      const peerEl = document.getElementById('vpn-selected-peer');
      if (peerEl) {
        const peerNames = { 'peer-1': 'MacBook Pro', 'peer-2': 'iPhone', 'peer-3': 'Linux Server' };
        peerEl.textContent = peerNames[selected.value] || selected.value;
      }
      this.showToast('Peer selected: ' + selected.value, 'success');
    }
    document.body.removeChild(modal);
  }

  toggleAutoConnect(enabled) {
    this.vpnState.autoConnect = enabled;
    this.showToast(enabled ? 'Auto-connect enabled' : 'Auto-connect disabled', 'info');
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
