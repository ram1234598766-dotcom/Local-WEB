// LocalWEB Desktop App - Reuses web GUI logic with native enhancements
import { LocalWEBApp } from '../pkg/gui/static/app.js';

// Wails v3 runtime bridge
let wailsRuntime = null;

// Check if running in Wails
function isWails() {
  return typeof window !== 'undefined' && window.wails !== undefined;
}

// Initialize Wails runtime
async function initWails() {
  if (isWails()) {
    try {
      const { default: runtime } = await import('@wailsapp/runtime');
      wailsRuntime = runtime;
      console.log('Wails runtime initialized');
    } catch (e) {
      console.warn('Wails runtime not available:', e);
    }
  }
}

// Enhanced LocalWEBApp for Desktop
class LocalWEBDesktopApp extends LocalWEBApp {
  constructor() {
    super();
    this.isDesktop = isWails();
    this.trayIcon = null;
    this.isMinimizedToTray = false;
  }

  async init() {
    await initWails();
    await super.init();
    
    if (this.isDesktop) {
      this.setupDesktopFeatures();
    }
  }

  setupDesktopFeatures() {
    // Listen for window events
    window.addEventListener('beforeunload', (e) => {
      if (!this.isQuitting) {
        e.preventDefault();
        this.minimizeToTray();
      }
    });

    // Listen for Wails events
    if (wailsRuntime) {
      wailsRuntime.EventsOn('tray-click', () => {
        this.showFromTray();
      });

      wailsRuntime.EventsOn('menu-action', (action) => {
        this.handleMenuAction(action);
      });
    }

    // Native drag-and-drop for files
    document.addEventListener('dragover', (e) => {
      e.preventDefault();
      e.stopPropagation();
      document.body.classList.add('drag-over');
    });

    document.addEventListener('dragleave', (e) => {
      e.preventDefault();
      e.stopPropagation();
      document.body.classList.remove('drag-over');
    });

    document.addEventListener('drop', (e) => {
      e.preventDefault();
      e.stopPropagation();
      document.body.classList.remove('drag-over');
      if (e.dataTransfer.files.length > 0) {
        this.uploadFiles(Array.from(e.dataTransfer.files));
      }
    });
  }

  minimizeToTray() {
    if (wailsRuntime) {
      wailsRuntime.WindowHide();
      this.isMinimizedToTray = true;
      this.showToast('Minimized to tray', 'info');
    }
  }

  showFromTray() {
    if (wailsRuntime) {
      wailsRuntime.WindowShow();
      wailsRuntime.WindowCenter();
      this.isMinimizedToTray = false;
    }
  }

  handleMenuAction(action) {
    switch (action) {
      case 'new-window':
        this.openNewWindow();
        break;
      case 'preferences':
        this.navigate('settings');
        break;
      case 'about':
        this.showAbout();
        break;
      case 'quit':
        this.quitApp();
        break;
    }
  }

  openNewWindow() {
    if (wailsRuntime) {
      wailsRuntime.WindowNew();
    }
  }

  showAbout() {
    this.showToast('LocalWEB Desktop v1.0.0', 'info');
  }

  quitApp() {
    this.isQuitting = true;
    if (wailsRuntime) {
      wailsRuntime.Quit();
    }
  }

  // Native notifications
  showNativeNotification(title, body) {
    if (wailsRuntime) {
      wailsRuntime.NotificationSend({
        title,
        body,
        icon: '/icon.png',
      });
    } else if ('Notification' in window && Notification.permission === 'granted') {
      new Notification(title, { body });
    }
  }

  // File system access
  async pickFiles(options = {}) {
    if (wailsRuntime) {
      return await wailsRuntime.DialogOpenFile({
        multiple: options.multiple || false,
        filters: options.filters || [],
      });
    } else if ('showOpenFilePicker' in window) {
      return await window.showOpenFilePicker(options);
    }
    return [];
  }

  async pickFolder() {
    if (wailsRuntime) {
      return await wailsRuntime.DialogOpenDirectory();
    } else if ('showDirectoryPicker' in window) {
      return await window.showDirectoryPicker();
    }
    return null;
  }

  async saveFile(options = {}) {
    if (wailsRuntime) {
      return await wailsRuntime.DialogSaveFile(options);
    } else if ('showSaveFilePicker' in window) {
      return await window.showSaveFilePicker(options);
    }
    return null;
  }

  // Auto-start management
  async setAutoStart(enabled) {
    if (wailsRuntime) {
      await wailsRuntime.SetAutoStart(enabled);
    }
  }

  // Check for updates
  async checkForUpdates() {
    if (wailsRuntime) {
      return await wailsRuntime.CheckForUpdates();
    }
    return { available: false };
  }

  // Window state management
  getWindowState() {
    return {
      x: window.screenX,
      y: window.screenY,
      width: window.outerWidth,
      height: window.outerHeight,
      maximized: window.outerWidth === screen.width && window.outerHeight === screen.height,
    };
  }

  restoreWindowState(state) {
    if (state) {
      window.moveTo(state.x, state.y);
      window.resizeTo(state.width, state.height);
      if (state.maximized) {
        window.maximize();
      }
    }
  }
}

// Initialize app
let app = null;

async function initApp() {
  await initWails();
  app = new LocalWEBDesktopApp();
  await app.init();
  
  // Make app globally accessible
  window.app = app;
  
  console.log('LocalWEB Desktop initialized');
}

// Export for testing
export { initApp, LocalWEBDesktopApp };