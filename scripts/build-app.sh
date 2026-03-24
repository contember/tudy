#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/build"
APP_DIR="$BUILD_DIR/Tudy.app"
CONTENTS_DIR="$APP_DIR/Contents"
MACOS_DIR="$CONTENTS_DIR/MacOS"
RESOURCES_DIR="$CONTENTS_DIR/Resources"

echo "==> Building Tudy.app..."

# Clean previous build
rm -rf "$APP_DIR"

# Create .app bundle structure
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"

# Build the binary
echo "==> Compiling binary..."
cd "$PROJECT_DIR/cmd/app"
CGO_ENABLED=1 go build -o "$MACOS_DIR/tudy" .
echo "    Built: $MACOS_DIR/tudy"

# Copy Info.plist
cp "$PROJECT_DIR/cmd/app/resources/Info.plist" "$CONTENTS_DIR/Info.plist"

# Generate .icns from PNG
ICON_PNG="$PROJECT_DIR/cmd/app/icons/tudy-app-icon-1024.png"
if [ -f "$ICON_PNG" ]; then
    echo "==> Generating app icon..."
    ICONSET_DIR="$BUILD_DIR/AppIcon.iconset"
    mkdir -p "$ICONSET_DIR"

    sips -z 16 16     "$ICON_PNG" --out "$ICONSET_DIR/icon_16x16.png"      > /dev/null 2>&1
    sips -z 32 32     "$ICON_PNG" --out "$ICONSET_DIR/icon_16x16@2x.png"   > /dev/null 2>&1
    sips -z 32 32     "$ICON_PNG" --out "$ICONSET_DIR/icon_32x32.png"      > /dev/null 2>&1
    sips -z 64 64     "$ICON_PNG" --out "$ICONSET_DIR/icon_32x32@2x.png"   > /dev/null 2>&1
    sips -z 128 128   "$ICON_PNG" --out "$ICONSET_DIR/icon_128x128.png"    > /dev/null 2>&1
    sips -z 256 256   "$ICON_PNG" --out "$ICONSET_DIR/icon_128x128@2x.png" > /dev/null 2>&1
    sips -z 256 256   "$ICON_PNG" --out "$ICONSET_DIR/icon_256x256.png"    > /dev/null 2>&1
    sips -z 512 512   "$ICON_PNG" --out "$ICONSET_DIR/icon_256x256@2x.png" > /dev/null 2>&1
    sips -z 512 512   "$ICON_PNG" --out "$ICONSET_DIR/icon_512x512.png"    > /dev/null 2>&1
    sips -z 1024 1024 "$ICON_PNG" --out "$ICONSET_DIR/icon_512x512@2x.png" > /dev/null 2>&1

    iconutil -c icns "$ICONSET_DIR" -o "$RESOURCES_DIR/AppIcon.icns"
    rm -rf "$ICONSET_DIR"
    echo "    Generated: $RESOURCES_DIR/AppIcon.icns"
else
    echo "    Warning: Icon PNG not found at $ICON_PNG, skipping icon generation"
fi

# Ad-hoc codesign
echo "==> Codesigning..."
codesign --force --deep --sign - "$APP_DIR"

echo ""
echo "==> Build complete: $APP_DIR"
echo "    Run with: open $APP_DIR"
