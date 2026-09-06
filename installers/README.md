# LocalWEB Installers

This directory contains platform-specific installer configurations and scripts for LocalWEB.

## Directory Structure

```
installers/
├── common/           # Shared assets (icons, licenses, etc.)
├── windows/          # Windows NSIS installer + Wintun driver
├── macos/            # macOS .app bundle, .dmg, .pkg
└── linux/            # nfpm configs, systemd units, scripts
```

## Platform Support Matrix

| Feature | Windows | macOS | Linux |
|---------|---------|-------|-------|
| **Daemon** | ✅ Windows Service | ✅ LaunchDaemon | ✅ systemd |
| **VPN (TUN)** | ✅ Wintun driver | ✅ Network Extension | ✅ CAP_NET_ADMIN |
| **BLE/WiFi Direct** | ✅ Native | ✅ Native | ⚠️ Stub/Partial |
| **Auto-start** | ✅ Service (auto) | ✅ LaunchDaemon | ✅ systemd |
| **Firewall** | ✅ netsh | ✅ socketfilterfw | ✅ ufw/firewalld/iptables |
| **Installer** | NSIS (.exe/.msi) | .app → .dmg | .deb/.rpm/.apk |
| **Signing** | Authenticode | Developer ID | GPG |
| **Notarization** | N/A | notarytool | N/A |

## Windows

### Prerequisites
- Windows 10/11 (64-bit)
- Administrator privileges
- Internet connection (for Wintun download)

### Files
- `installers/windows/localweb.nsi` - NSIS installer script
- `installers/windows/wintun/wintun.dll` - Wintun TUN driver (downloaded at build)
- `installers/windows/ServiceInstall.ps1` - Windows Service management
- `installers/windows/preinstall.ps1` / `postinstall.ps1` / `preremove.ps1` / `postremove.ps1`

### Build
```bash
# On Windows
choco install nsis
makensis installers/windows/localweb.nsi
```

### Installation
Run the generated `LocalWEB-Setup.exe` as Administrator.

### Post-Install
- LocalWEB service starts automatically
- Wintun driver installed to `C:\Windows\System32\drivers\wintun.dll`
- Firewall rules created for ports 4443 (QUIC), 5353 (mDNS), 8080 (GUI)
- Start Menu shortcuts created
- Desktop shortcut created

### Uninstall
Use "Add or Remove Programs" or run the uninstaller from Start Menu.

## macOS

### Prerequisites
- macOS 13.0 (Ventura) or later
- Apple Silicon (arm64) or Intel (x86_64)
- Apple Developer Program membership (for signing/notarization)

### Files
- `installers/macos/Info.plist` - App bundle metadata with privacy descriptions
- `installers/macos/Entitlements.plist` - Network Extension, Bluetooth, Local Network
- `installers/macos/LaunchDaemon.plist` - LaunchDaemon for background daemon
- `installers/macos/preinstall.sh` / `postinstall.sh` - pkg scripts
- `installers/macos/dmg-background.png` - DMG background image
- `installers/macos/icon.icns` - App icon

### Build
```bash
# Create .app bundle
mkdir -p LocalWEB.app/Contents/MacOS
mkdir -p LocalWEB.app/Contents/Resources
cp localweb LocalWEB.app/Contents/MacOS/localweb
cp localweb-cli LocalWEB.app/Contents/MacOS/localweb-cli
cp installers/macos/Info.plist LocalWEB.app/Contents/Info.plist
cp installers/macos/Entitlements.plist LocalWEB.app/Contents/Entitlements.plist
cp installers/macos/LaunchDaemon.plist LocalWEB.app/Contents/Resources/LaunchDaemon.plist
cp installers/macos/icon.icns LocalWEB.app/Contents/Resources/AppIcon.icns

# Code sign (requires Apple Developer ID)
codesign --force --deep --sign "Developer ID Application: <Team ID>" --entitlements installers/macos/Entitlements.plist LocalWEB.app

# Create DMG
hdiutil create -volname "LocalWEB" -srcfolder LocalWEB.app -ov -format UDZO LocalWEB.dmg

# Notarize (requires Apple ID, Team ID, App Password)
xcrun notarytool submit LocalWEB.dmg --apple-id "<id>" --team-id "<team>" --password "<pw>" --wait
xcrun stapler staple LocalWEB.dmg
```

### Installation
Open `LocalWEB.dmg` and drag `LocalWEB.app` to Applications.

### Post-Install
- LaunchDaemon loaded automatically at boot
- First launch prompts for:
  - Local Network access
  - Bluetooth permission
  - Network Extension (VPN) approval in System Settings
- CLI available at `/usr/local/bin/localweb-cli`

### VPN on macOS
**Important**: The VPN service requires the **Network Extension** entitlement (`com.apple.developer.networking.networkextension`). A plain TUN binary cannot create system VPN tunnels on macOS 13+.

If you don't have an Apple Developer Program membership:
- VPN service will not work
- Other features (messaging, files, docs) work normally
- Document this limitation for users

## Linux

### Prerequisites
- systemd (247+)
- Kernel 5.10+ (for WireGuard/Wintun equivalent)
- CAP_NET_ADMIN capability (via setcap, not root)

### Files
- `nfpm.yaml` - Package configuration
- `systemd/localweb.service` - Systemd service with hardening
- `systemd/localweb@.service` - Template for multi-instance
- `installers/linux/preinstall.sh` / `postinstall.sh` / `preremove.sh` / `postremove.sh`
- RPM/APK specific scripts in `installers/linux/`

### Build
```bash
# Install nfpm
curl -sSL https://github.com/goreleaser/nfpm/releases/download/v2.35.0/nfpm_2.35.0_linux_amd64.deb -o nfpm.deb
sudo dpkg -i nfpm.deb

# Build packages
nfpm package --config nfpm.yaml --packager deb --target pkg/
nfpm package --config nfpm.yaml --packager rpm --target pkg/
nfpm package --config nfpm.yaml --packager apk --target pkg/
```

### Installation

**Debian/Ubuntu:**
```bash
sudo dpkg -i localweb_1.0.0_linux_amd64.deb
sudo apt-get install -f
```

**RHEL/Fedora:**
```bash
sudo rpm -i localweb-1.0.0-1.x86_64.rpm
```

**Arch:**
```bash
sudo pacman -U localweb-1.0.0-1-x86_64.pkg.tar.zst
```

**Alpine:**
```bash
apk add localweb-1.0.0-r1.apk
```

### Post-Install
```bash
# Start and enable
sudo systemctl start localweb
sudo systemctl enable localweb

# Check status
systemctl status localweb
journalctl -u localweb -f

# Config
cat /etc/localweb/config.json
```

### Capabilities (No Root Required)
The binaries are installed with capabilities instead of running as root:
```bash
setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb
```

### Firewall
Auto-configured for:
- `ufw` (Ubuntu/Debian)
- `firewalld` (RHEL/Fedora)
- `iptables` (fallback)

Ports opened:
- `4443/udp` - QUIC transport
- `5353/udp` - mDNS
- `8080/tcp` - Web GUI

## CI/CD Pipeline

The `.github/workflows/ci.yml` handles:
1. **Quality Gates**: lint, test, govulncheck
2. **Cross-compilation**: 5 platform/arch combinations
3. **Packaging**: Linux (.deb/.rpm/.apk), Windows (.msi), macOS (.dmg)
4. **Docker**: Multi-arch images to ghcr.io
5. **Release**: Auto-create GitHub Release with artifacts + checksums
6. **Verification**: Checksum verification, container test install

### Trigger Release
```bash
git tag v1.0.0
git push origin v1.0.0
```

### Manual Release (no CI)
```bash
# Local build
goreleaser release --clean --snapshot
```

## Security Considerations

### Windows
- **SmartScreen**: Unsigned installer triggers SmartScreen warning
- **Workaround**: "More info" → "Run anyway"
- **Fix**: Purchase Authenticode certificate, configure CI secrets

### macOS
- **Gatekeeper**: Unsigned app shows "unidentified developer"
- **Workaround**: Right-click → Open, or `xattr -d com.apple.quarantine`
- **Network Extension**: Requires Apple Developer Program ($99/yr)
- **Fix**: Enroll in Apple Developer Program, configure CI secrets

### Linux
- **Capabilities**: Uses `setcap` instead of root
- **SELinux**: Policy included in nfpm config
- **AppArmor**: Profile included in nfpm config

## Troubleshooting

### Windows
- **Service won't start**: Check Wintun driver in Device Manager
- **Firewall blocks**: Verify netsh rules with `netsh advfirewall firewall show rule name=LocalWEB`
- **Port conflict**: Change port in config or stop conflicting service

### macOS
- **App won't open**: Right-click → Open, or `xattr -d com.apple.quarantine`
- **VPN not working**: Check Network Extension approval in System Settings
- **Bluetooth not working**: Check Privacy & Security → Bluetooth permission

### Linux
- **Service fails**: `journalctl -u localweb -f`
- **Capabilities lost**: Re-run `setcap` on binaries
- **Firewall blocks**: Check ufw/firewalld/iptables rules

## Signing Certificates

### Windows (Authenticode)
```bash
# Requires: EV Code Signing Certificate (.pfx)
signtool sign /f cert.pfx /p <password> /fd sha256 /tr http://timestamp.digicert.com /td sha256 localweb.exe
```

### macOS
```bash
# Requires: Apple Developer ID Application + Installer certificates
codesign --force --deep --sign "Developer ID Application: <Team>" --entitlements Entitlements.plist LocalWEB.app
xcrun notarytool submit LocalWEB.dmg --apple-id <id> --team-id <team> --password <pw> --wait
xcrun stapler staple LocalWEB.dmg
```

### Linux (GPG)
```bash
# GPG signing
gpg --armor --detach-sign --output localweb_1.0.0_amd64.deb.sig localweb_1.0.0_amd64.deb
```

## Versioning

Version is determined by git tag:
```bash
git tag v1.0.0
git push origin v1.0.0
```

Goreleaser reads the tag and injects version/commit/date into binaries.

## Release Checklist

- [ ] All tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Govulncheck clean (`make govulncheck`)
- [ ] Version tag created and pushed
- [ ] CI pipeline completes all platforms
- [ ] Installers tested on clean VMs
- [ ] Checksums verified
- [ ] Release notes updated
- [ ] GitHub Release published

## Support

- GitHub Issues: https://github.com/ram1234598766-dotcom/Local-WEB/issues
- Documentation: https://github.com/ram1234598766-dotcom/Local-WEB/docs
- Security: security@localweb.io