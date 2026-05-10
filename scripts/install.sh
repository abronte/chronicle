#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.local/bin"

echo "Building chronicle..."
go build -o bin/chronicle ./cmd/chronicle

mkdir -p "$INSTALL_DIR"
cp bin/chronicle "$INSTALL_DIR/chronicle"
chmod +x "$INSTALL_DIR/chronicle"

echo "Installed chronicle to $INSTALL_DIR/chronicle"

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
	echo "Note: $INSTALL_DIR is not in your PATH."
	echo "Add the following to your shell config (~/.zshrc, ~/.bashrc, etc.):"
	echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi