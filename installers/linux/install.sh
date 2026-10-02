#!/usr/bin/env bash
# LocalWEB Linux Installer Script
# Auto-detects distro and installs the appropriate package

set -euo pipefail

REPO="ram1234598766-dotcom/Local-WEB"
BINARY_NAME="localweb"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/localweb"
DATA_DIR="/var/lib/localweb"
SERVICE_NAME="localweb"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

# Detect Linux distribution
detect_distro() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        DISTRO=$ID
        VERSION=$VERSION_ID
    else
        log_error "Cannot detect Linux distribution"
        exit 1
    fi
    log_info "Detected distribution: $DISTRO $VERSION"
}

# Detect architecture
detect_arch() {
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
    esac
    log_info "Architecture: $ARCH"
}

# Get latest release version from GitHub
get_latest_version() {
    log_info "Fetching latest release version..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')
    if [[ -z "$VERSION" ]]; then
        log_error "Failed to fetch latest version"
        exit 1
    fi
    log_info "Latest version: $VERSION"
}

# Download and install package based on distro
install_package() {
    local url=""
    local pkg_file=""
    
    case $DISTRO in
        ubuntu|debian|linuxmint|pop|elementary|kali|raspbian)
            url="https://github.com/$REPO/releases/download/$VERSION/localweb_${VERSION#v}_linux_${ARCH}.deb"
            pkg_file="/tmp/localweb.deb"
            log_info "Downloading .deb package..."
            curl -fsSL "$url" -o "$pkg_file"
            log_info "Installing .deb package..."
            dpkg -i "$pkg_file" || apt-get install -f -y
            ;;
        fedora|rhel|centos|rocky|almalinux|nobara)
            url="https://github.com/$REPO/releases/download/$VERSION/localweb-${VERSION#v}-1.${ARCH}.rpm"
            pkg_file="/tmp/localweb.rpm"
            log_info "Downloading .rpm package..."
            curl -fsSL "$url" -o "$pkg_file"
            log_info "Installing .rpm package..."
            rpm -i "$pkg_file" || dnf install -y "$pkg_file"
            ;;
        arch|manjaro|endeavouros|garuda)
            url="https://github.com/$REPO/releases/download/$VERSION/localweb-${VERSION#v}-1-${ARCH}.pkg.tar.zst"
            pkg_file="/tmp/localweb.pkg.tar.zst"
            log_info "Downloading Arch package..."
            curl -fsSL "$url" -o "$pkg_file"
            log_info "Installing Arch package..."
            pacman -U --noconfirm "$pkg_file"
            ;;
        alpine)
            url="https://github.com/$REPO/releases/download/$VERSION/localweb-${VERSION#v}-r1.apk"
            pkg_file="/tmp/localweb.apk"
            log_info "Downloading Alpine package..."
            curl -fsSL "$url" -o "$pkg_file"
            log_info "Installing Alpine package..."
            apk add --allow-untrusted "$pkg_file"
            ;;
        *)
            log_error "Unsupported distribution: $DISTRO"
            log_info "Supported: Ubuntu/Debian, Fedora/RHEL, Arch, Alpine"
            exit 1
            ;;
    esac
}

# Configure systemd service
configure_service() {
    log_info "Configuring systemd service..."
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
    
    # Wait a moment for service to start
    sleep 2
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        log_success "Service $SERVICE_NAME started successfully"
    else
        log_warn "Service may not have started. Check: journalctl -u $SERVICE_NAME -f"
    fi
}

# Set capabilities on binary
set_capabilities() {
    if command -v setcap >/dev/null 2>&1; then
        log_info "Setting capabilities on binaries..."
        setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb 2>/dev/null || true
        setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb-cli 2>/dev/null || true
    fi
}

# Ports the node actually listens on by default.
#
# These used to be spelled out separately in each of the three firewall
# backends below, which is how they drifted: 8080 was opened even though the
# dashboard binds loopback by default, while every real service port was left
# closed. Listing them once keeps the backends in agreement with cmd/node.
#
# UDP: 4443 QUIC, 5353 DNS/mDNS discovery
# TCP: 8082 HTTP gateway, 587 SMTP, 993 IMAP, 9092 registry, 9094 DHT
#
# The dashboard's 8080 is deliberately absent. It is unauthenticated and can
# write files and restore backups, so it binds 127.0.0.1 unless an operator
# passes -gui-addr 0.0.0.0:8080 deliberately.
LOCALWEB_UDP_PORTS="4443 5353"
LOCALWEB_TCP_PORTS="8082 587 993 9092 9094"

# Configure firewall
configure_firewall() {
    log_info "Configuring firewall..."

    if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
        for port in $LOCALWEB_UDP_PORTS; do
            ufw allow "$port/udp" comment "LocalWEB" >/dev/null 2>&1 || true
        done
        for port in $LOCALWEB_TCP_PORTS; do
            ufw allow "$port/tcp" comment "LocalWEB" >/dev/null 2>&1 || true
        done
        log_info "UFW rules added"
    elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
        for port in $LOCALWEB_UDP_PORTS; do
            firewall-cmd --permanent --add-port="$port/udp" >/dev/null 2>&1 || true
        done
        for port in $LOCALWEB_TCP_PORTS; do
            firewall-cmd --permanent --add-port="$port/tcp" >/dev/null 2>&1 || true
        done
        firewall-cmd --reload >/dev/null 2>&1 || true
        log_info "Firewalld rules added"
    elif command -v iptables >/dev/null 2>&1; then
        for port in $LOCALWEB_UDP_PORTS; do
            iptables -A INPUT -p udp --dport "$port" -j ACCEPT 2>/dev/null || true
        done
        for port in $LOCALWEB_TCP_PORTS; do
            iptables -A INPUT -p tcp --dport "$port" -j ACCEPT 2>/dev/null || true
        done
        if command -v iptables-save >/dev/null 2>&1; then
            iptables-save > /etc/iptables/rules.v4 2>/dev/null || true
        fi
        log_info "iptables rules added"
    else
        log_warn "No supported firewall found. Open UDP $LOCALWEB_UDP_PORTS and TCP $LOCALWEB_TCP_PORTS manually."
    fi

    log_info "The dashboard is not opened: it binds 127.0.0.1 by default."
    log_info "To reach it from another machine, start the node with -gui-addr 0.0.0.0:8080"
    log_info "and put your own authentication in front of it."
}

# Print node ID and next steps
print_summary() {
    log_info "Waiting for node to generate identity..."
    sleep 3
    
    if [[ -f /var/lib/localweb/identity.json ]]; then
        NODE_ID=$(grep -o '"node_id":"[^"]*"' /var/lib/localweb/identity.json | cut -d'"' -f4)
        if [[ -n "$NODE_ID" ]]; then
            log_success "Installation complete!"
            echo
            echo "Your Node ID: ${GREEN}$NODE_ID${NC}"
            echo
            echo "Next steps:"
            echo "  - Check status: systemctl status $SERVICE_NAME"
            echo "  - View logs: journalctl -u $SERVICE_NAME -f"
            echo "  - Config: $CONFIG_DIR/config.json"
            echo "  - On another machine, run: localweb-cli peers"
        else
            log_warn "Node ID not found yet. Check logs: journalctl -u $SERVICE_NAME -f"
        fi
    else
        log_warn "Identity file not found yet. Node may still be starting."
    fi
    
    echo
    echo "Useful commands:"
    echo "  localweb-cli peers          # List connected peers"
    echo "  localweb-cli services       # List available services"
    echo "  systemctl status $SERVICE_NAME"
    echo "  journalctl -u $SERVICE_NAME -f"
}

# Main
main() {
    echo "============================================"
    echo "  LocalWEB Linux Installer"
    echo "============================================"
    echo
    
    check_root
    detect_distro
    detect_arch
    get_latest_version
    install_package
    set_capabilities
    configure_firewall
    configure_service
    print_summary
}

main "$@"