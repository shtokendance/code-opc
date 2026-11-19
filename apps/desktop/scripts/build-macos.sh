#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${DESKTOP_DIR}/dist"
APP_NAME="Multica"
BUNDLE_ID="ai.multica.desktop"
VERSION="$(node -p "require('${DESKTOP_DIR}/package.json').version")"
ARCH="$(uname -m)"

echo "==> [1/6] Building frontend assets..."
cd "${DESKTOP_DIR}"
pnpm exec electron-vite build

echo "==> [2/6] Preparing macOS App Bundle structure..."
APP_BUNDLE="${DIST_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

rm -rf "${APP_BUNDLE}" "${DIST_DIR}/${APP_NAME}-${VERSION}-${ARCH}.zip" "${DIST_DIR}/${APP_NAME}-${VERSION}-${ARCH}.dmg" "${DIST_DIR}/dmg-staging"
mkdir -p "${MACOS_DIR}" "${RESOURCES_DIR}"

echo "==> [3/6] Compiling native Go binary..."
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "${MACOS_DIR}/${APP_NAME}" .
chmod +x "${MACOS_DIR}/${APP_NAME}"

echo "==> [4/6] Copying resources and generating metadata..."
if [ -f "${DESKTOP_DIR}/build/icon.icns" ]; then
    cp "${DESKTOP_DIR}/build/icon.icns" "${RESOURCES_DIR}/icon.icns"
fi

cat <<EOF > "${CONTENTS_DIR}/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>icon.icns</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSHumanReadableCopyright</key>
    <string>Copyright © 2026 Multica. All rights reserved.</string>
</dict>
</plist>
EOF

echo -n "APPL????" > "${CONTENTS_DIR}/PkgInfo"

if command -v codesign &>/dev/null; then
    echo "==> [5/6] Ad-hoc code signing the .app bundle..."
    if [ -f "${DESKTOP_DIR}/build/entitlements.mac.plist" ]; then
        codesign --force --deep --sign - --entitlements "${DESKTOP_DIR}/build/entitlements.mac.plist" "${APP_BUNDLE}"
    else
        codesign --force --deep --sign - "${APP_BUNDLE}"
    fi
fi

echo "==> [6/6] Generating distribution packages (DMG & ZIP)..."
# 1. Zip
cd "${DIST_DIR}"
ZIP_NAME="${APP_NAME}-${VERSION}-${ARCH}.zip"
zip -r -y -q "${ZIP_NAME}" "${APP_NAME}.app"

# 2. DMG
DMG_NAME="${APP_NAME}-${VERSION}-${ARCH}.dmg"
DMG_STAGING="${DIST_DIR}/dmg-staging"
rm -rf "${DMG_STAGING}"
mkdir -p "${DMG_STAGING}"
cp -R "${APP_BUNDLE}" "${DMG_STAGING}/"
ln -s /Applications "${DMG_STAGING}/Applications"

hdiutil create -volname "${APP_NAME}" -srcfolder "${DMG_STAGING}" -ov -format UDZO "${DMG_NAME}"
rm -rf "${DMG_STAGING}"

echo "=========================================================="
echo " Packaging completed successfully!"
echo " Artifacts generated in: ${DIST_DIR}"
echo "   - App Bundle: ${APP_BUNDLE}"
echo "   - DMG:        ${DIST_DIR}/${DMG_NAME}"
echo "   - ZIP:        ${DIST_DIR}/${ZIP_NAME}"
echo "=========================================================="
