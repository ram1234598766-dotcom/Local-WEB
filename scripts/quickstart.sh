#!/usr/bin/env bash
#
# quickstart.sh — Build Local-WEB, generate identity, start the node.
# One command from clone to running.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$REPO_ROOT"

echo "=== Local-WEB Quickstart ==="
echo ""
echo "Building node and CLI binaries..."
go build -o bin/localweb ./cmd/node
go build -o bin/localweb-cli ./cmd/cli

echo ""
echo "Generating node identity (stored in ~/.localweb/)..."
./bin/localweb-cli id || true

echo ""
echo "Starting node..."
echo ""
echo "============================================"
echo "  Your node is now running."
echo "  Node ID: $(./bin/localweb-cli id 2>/dev/null | grep 'Node ID' || echo 'see log below')"
echo ""
echo "  On another machine on the same network:"
echo "    1. Run this same quickstart script"
echo "    2. Run:  ./bin/localweb-cli peers"
echo ""
echo "  Press Ctrl+C to stop."
echo "============================================"
echo ""

exec ./bin/localweb --name "$(hostname)"
