#!/usr/bin/env bash
# LocalWEB macOS Installer Script
# Downloads and installs the .dmg from GitHub Releases

set -euo pipefail

REPO="ram1234598766-dotcom/Local-WEB"
APP_NAME="LocalWEB"
DMG_NAME="LocalWEB.dmg"
APP_PATH="/Applications/${APP_NAME}.app"
MOUNT_POINT="/Volumes/${APP_NAME}"
DMG_URL=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# Check macOS version
check_macos_version() {
    local version=$(sw_vers -productVersion)
    local major=$(echo "$version" | cut -d. -f1)
    local minor=$(echo "$version" | cut -d. -f2)
    
    if [[ $major -lt 13 ]] || [[ $major -eq 13 && $minor -lt 0 ]]; then
        log_error "macOS 13.0 (Ventura) or later required. Current: $version"
        exit 1
    fi
    log_info "macOS version: $version"
}

# Check architecture
check_arch() {
    ARCH=$(uname -m)
    if [[ "$ARCH" != "arm64" && "$ARCH" != "x86_64" ]]; then
        log_error "Unsupported architecture: $ARCH (requires arm64 or x86_64)"
        exit 1
    fi
    log_info "Architecture: $ARCH"
}

# Get latest release version and DMG URL
get_dmg_url() {
    log_info "Fetching latest release..."
    local api_response=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")
    
    VERSION=$(echo "$api_response" | grep '"tag_name":' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')
    if [[ -z "$VERSION" ]]; then
        log_error "Failed to fetch latest version"
        exit 1
    fi
    
    # Find DMG asset
    DMG_URL=$(echo "$api_response" | grep -A 20 '"assets":' | grep '"browser_download_url":' | grep '\.dmg"' | head -1 | sed -E 's/.*"browser_download_url": "([^"]+)".*/\1/')
    
    if [[ -z "$DMG_URL" ]]; then
        log_error "No .dmg asset found in latest release"
        exit 1
    fi
    
    log_info "Latest version: $VERSION"
    log_info "DMG URL: $DMG_URL"
}

# Download DMG
download_dmg() {
    local dmg_file="/tmp/${DMG_NAME}"
    
    log_info "Downloading ${DMG_NAME}..."
    if ! curl -fsSL "$DMG_URL" -o "$dmg_file"; then
        log_error "Failed to download DMG"
        exit 1
    fi
    
    if [[ ! -s "$dmg_file" ]]; then
        log_error "Downloaded file is empty"
        exit 1
    fi
    
    log_success "Downloaded $(du -h "$dmg_file" | cut -f1)"
    DMG_FILE="$dmg_file"
}

# Install DMG
install_dmg() {
    log_info "Mounting DMG..."
    hdiutil attach "$DMG_FILE" -nobrowse -quiet
    
    if [[ ! -d "$MOUNT_POINT" ]]; then
        # Try to find mount point
        MOUNT_POINT=$(hdiutil info | grep -A 5 "${APP_NAME}" | grep '/Volumes/' | awk '{print $1}')
    fi
    
    if [[ ! -d "$MOUNT_POINT" ]]; then
        log_error "Failed to mount DMG"
        exit 1
    fi
    
    log_info "Installing to Applications..."
    
    # Remove existing app
    if [[ -d "$APP_PATH" ]]; then
        log_info "Removing existing installation..."
        rm -rf "$APP_PATH"
    fi
    
    cp -R "$MOUNT_POINT/${APP_NAME}.app" /Applications/
    
    log_info "Unmounting DMG..."
    hdiutil detach "$MOUNT_POINT" -quiet >/dev/null 2>&1 || true
    
    # Remove DMG file
    rm -f "$DMG_FILE"
    
    log_success "Installed to $APP_PATH"
}

# Configure LaunchDaemon
configure_launchdaemon() {
    log_info "Configuring LaunchDaemon..."
    
    local plist_path="/Library/LaunchDaemons/com.localweb.daemon.plist"
    
    if [[ -f "$APP_PATH/Contents/Resources/LaunchDaemon.plist" ]]; then
        sudo cp "$APP_PATH/Contents/Resources/LaunchDaemon.plist" "$plist_path"
        sudo chown root:wheel "$plist_path"
        sudo chmod 644 "$plist_path"
        
        # Load daemon
        sudo launchctl load "$plist_path" 2>/dev/null || true
        sleep 2
        
        if launchctl list | grep -q "com.localweb.daemon"; then
            log_success "LaunchDaemon loaded"
        else
            log_warn "LaunchDaemon may not have started. Check: sudo launchctl load $plist_path"
        fi
    else
        log_warn "LaunchDaemon.plist not found in app bundle"
    fi
}

# Create CLI symlink
create_cli_symlink() {
    local cli_src="$APP_PATH/Contents/MacOS/localweb-cli"
    local cli_dst="/usr/local/bin/localweb-cli"
    
    if [[ -f "$cli_src" ]]; then
        log_info "Creating CLI symlink..."
        sudo ln -sf "$cli_src" "$cli_dst"
        log_success "CLI available at $cli_dst"
    fi
}

# Request permissions
request_permissions() {
    log_warn "IMPORTANT: You need to grant permissions manually:"
    echo
    echo "1. System Settings → Privacy & Security → Local Network → Enable '$APP_NAME'"
    echo "2. System Settings → Privacy & Security → Bluetooth → Enable '$APP_NAME'"
    echo "3. For VPN: System Settings → General → VPN & Network → Allow Network Extension for '$APP_NAME'"
    echo
    log_info "Open System Settings now? (y/n)"
    read -r response
    if [[ "$response" =~ ^[Yy]$ ]]; then
        open "x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork"
    fi
}

# Print summary
print_summary() {
    log_info "Waiting for node to generate identity..."
    sleep 3
    
    local data_dir="$HOME/Library/Application Support/LocalWEB"
    if [[ -f "$data_dir/identity.json" ]]; then
        NODE_ID=$(grep -o '"node_id":"[^"]*"' "$data_dir/identity.json" 2>/dev/null | cut -d'"' -f4 || true)
    fi
    
    log_success "Installation complete!"
    echo
    
    if [[ -n "$NODE_ID" ]]; then
        echo "Your Node ID: ${GREEN}$NODE_ID${NC}"
    fi
    
    echo
    echo "Next steps:"
    echo "  - Grant permissions (see above)"
    echo "  - View logs: tail -f /var/log/localweb.log"
    echo "  - CLI: localweb-cli peers"
    echo "  - Start daemon: sudo launchctl load /Library/LaunchDaemons/com.localweb.daemon.plist"
    echo "  - Stop daemon: sudo launchctl unload /Library/LaunchDaemons/com.localweb.daemon.plist"
    echo
    echo "On another machine, run: localweb-cli peers"
}

# Main
main() {
    echo "============================================"
    echo "  LocalWEB macOS Installer"
    echo "============================================"
    echo
    
    check_macos_version
    check_arch
    get_dmg_url
    download_dmg
    install_dmg
    configure_launchdaemon
    create_cli_symlink
    request_permissions
    print_summary
}

main "$@"