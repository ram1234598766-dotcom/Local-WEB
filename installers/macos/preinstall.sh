#!/bin/bash
# macOS pre-install script for LocalWEB .pkg
# Runs before installation to check prerequisites

set -e

echo "LocalWEB pre-installation checks..."

# Check macOS version (requires 13.0+)
OS_VERSION=$(sw_vers -productVersion)
MAJOR_VERSION=$(echo $OS_VERSION | cut -d. -f1)
MINOR_VERSION=$(echo $OS_VERSION | cut -d. -f2)

if [ "$MAJOR_VERSION" -lt 13 ]; then
    echo "ERROR: LocalWEB requires macOS 13.0 (Ventura) or later"
    exit 1
fi

# Check architecture
ARCH=$(uname -m)
if [ "$ARCH" != "arm64" ] && [ "$ARCH" != "x86_64" ]; then
    echo "ERROR: LocalWEB requires Apple Silicon (arm64) or Intel (x86_64)"
    exit 1
fi

# Check for existing installation
if [ -d "/Applications/LocalWEB.app" ]; then
    echo "Existing installation found at /Applications/LocalWEB.app"
    # Stop any running daemon
    launchctl unload /Library/LaunchDaemons/com.localweb.daemon.plist 2>/dev/null || true
    pkill -f "LocalWEB" 2>/dev/null || true
    sleep 2
fi

# Check for existing daemon
if launchctl list | grep -q "com.localweb.daemon"; then
    echo "Stopping existing LocalWEB daemon..."
    launchctl unload /Library/LaunchDaemons/com.localweb.daemon.plist 2>/dev/null || true
fi

echo "Pre-install checks passed"
exit 0