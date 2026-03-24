#!/bin/bash
set -e

REPO="contember/tudy"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest version
if [ -z "$VERSION" ]; then
  VERSION=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
fi

if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version"
  exit 1
fi

echo "Installing tudy v${VERSION} for ${OS}/${ARCH}..."

# Download and extract
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/tudy-${OS}-${ARCH}.tar.gz"
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

echo "Downloading from ${DOWNLOAD_URL}..."
curl -sL "$DOWNLOAD_URL" | tar xz -C "$TMP_DIR"

# Install binaries
SUDO=""
if [ ! -w "$INSTALL_DIR" ]; then
  SUDO="sudo"
  echo "Installing to $INSTALL_DIR (requires sudo)..."
fi

$SUDO mkdir -p "$INSTALL_DIR"

# CLI binary → tudy, Caddy binary → tudy-bin
$SUDO mv "$TMP_DIR/cli" "$INSTALL_DIR/tudy"
$SUDO mv "$TMP_DIR/caddy" "$INSTALL_DIR/tudy-bin"
$SUDO chmod +x "$INSTALL_DIR/tudy" "$INSTALL_DIR/tudy-bin"

# macOS: remove quarantine and sign
if [ "$OS" = "darwin" ]; then
  $SUDO xattr -d com.apple.quarantine "$INSTALL_DIR/tudy" 2>/dev/null || true
  $SUDO xattr -d com.apple.quarantine "$INSTALL_DIR/tudy-bin" 2>/dev/null || true
  $SUDO codesign --force --deep --sign - "$INSTALL_DIR/tudy" 2>/dev/null || true
  $SUDO codesign --force --deep --sign - "$INSTALL_DIR/tudy-bin" 2>/dev/null || true
fi

echo ""
echo "Installed tudy v${VERSION} to $INSTALL_DIR"
echo ""
echo "Run the setup wizard:"
echo "  tudy setup"
