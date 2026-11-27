#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${DESKTOP_DIR}/dist"
APP_NAME="CodeOPC"
BUNDLE_ID="ai.codeopc.desktop"
VERSION="$(node -p "require('${DESKTOP_DIR}/package.json').version")"
ARCH="$(uname -m)"
DESKTOP_ENV="${DESKTOP_ENV:-prod}"
ENDPOINTS_FILE="${DESKTOP_DIR}/config/endpoints.${DESKTOP_ENV}.json"

if [ ! -f "${ENDPOINTS_FILE}" ]; then
    echo "Error: Endpoints template not found: ${ENDPOINTS_FILE}" >&2
    exit 1
fi

DEFAULT_API_URL="$(node -p "require('${ENDPOINTS_FILE}').apiUrl")"
DEFAULT_APP_URL="$(node -p "require('${ENDPOINTS_FILE}').appUrl")"
DEFAULT_PLATFORM_URL="$(node -p "require('${ENDPOINTS_FILE}').platformUrl")"

echo "==> [1/6] Building frontend assets (env=${DESKTOP_ENV}, api=${DEFAULT_API_URL})..."
cd "${DESKTOP_DIR}"
VITE_DEFAULT_API_URL="${DEFAULT_API_URL}" \
VITE_DEFAULT_APP_URL="${DEFAULT_APP_URL}" \
VITE_DEFAULT_PLATFORM_URL="${DEFAULT_PLATFORM_URL}" \
pnpm exec electron-vite build

echo "==> [2/6] Preparing macOS App Bundle structure..."
APP_BUNDLE="${DIST_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

rm -rf "${APP_BUNDLE}" "${DIST_DIR}/${APP_NAME}-${VERSION}-${ARCH}.zip" "${DIST_DIR}/${APP_NAME}-${VERSION}-${ARCH}.dmg" "${DIST_DIR}/dmg-staging"
mkdir -p "${MACOS_DIR}" "${RESOURCES_DIR}"

echo "==> [3/6] Compiling native Go binary..."
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.appVersion=${VERSION} -X main.appProductName=${APP_NAME} -X main.defaultAPIURL=${DEFAULT_API_URL} -X main.defaultAppURL=${DEFAULT_APP_URL} -X main.defaultPlatformURL=${DEFAULT_PLATFORM_URL}" -o "${MACOS_DIR}/${APP_NAME}" .
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
    <key>CFBundleURLTypes</key>
    <array>
        <dict>
            <key>CFBundleURLName</key>
            <string>${APP_NAME}</string>
            <key>CFBundleURLSchemes</key>
            <array>
                <string>codeopc</string>
                <string>onepc</string>
                <string>multica</string>
            </array>
        </dict>
    </array>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSHumanReadableCopyright</key>
    <string>Copyright © 2026 CodeOPC. All rights reserved.</string>
</dict>
</plist>
EOF

echo -n "APPL????" > "${CONTENTS_DIR}/PkgInfo"

if command -v codesign &>/dev/null; then
    SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-}"
    if [ -z "${SIGN_IDENTITY}" ]; then
        SIGN_IDENTITY="$(security find-identity -v -p codesigning 2>/dev/null | sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -n 1 || true)"
    fi
    ENTITLEMENTS_ARGS=()
    if [ -f "${DESKTOP_DIR}/build/entitlements.mac.plist" ]; then
        ENTITLEMENTS_ARGS=(--entitlements "${DESKTOP_DIR}/build/entitlements.mac.plist")
    fi
    if [ -n "${SIGN_IDENTITY}" ]; then
        echo "==> [5/6] Signing .app bundle with '${SIGN_IDENTITY}'..."
        codesign --force --deep --options runtime --timestamp --sign "${SIGN_IDENTITY}" "${ENTITLEMENTS_ARGS[@]}" "${APP_BUNDLE}"
        codesign --verify --deep --strict --verbose=2 "${APP_BUNDLE}"
    elif [ "${REQUIRE_APPLE_SIGNING:-0}" = "1" ]; then
        echo "Error: Developer ID Application certificate is required for production release signing." >&2
        exit 1
    else
        echo "==> [5/6] Ad-hoc code signing the .app bundle..."
        codesign --force --deep --sign - "${ENTITLEMENTS_ARGS[@]}" "${APP_BUNDLE}"
    fi
fi

NOTARY_AUTH_ARGS=()
if [ -n "${SIGN_IDENTITY:-}" ] && command -v xcrun &>/dev/null; then
    if [ -n "${APPLE_NOTARY_PROFILE:-}" ]; then
        NOTARY_AUTH_ARGS=(--keychain-profile "${APPLE_NOTARY_PROFILE}")
    elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_APP_SPECIFIC_PASSWORD:-}" ]; then
        TEAM_ID="${APPLE_TEAM_ID:-}"
        if [ -z "${TEAM_ID}" ]; then
            TEAM_ID="$(echo "${SIGN_IDENTITY}" | sed -n 's/.*(\([A-Z0-9]\{10\}\))$/\1/p')"
        fi
        if [ -n "${TEAM_ID}" ]; then
            NOTARY_AUTH_ARGS=(--apple-id "${APPLE_ID}" --password "${APPLE_APP_SPECIFIC_PASSWORD}" --team-id "${TEAM_ID}")
        fi
    fi

    if [ ${#NOTARY_AUTH_ARGS[@]} -gt 0 ]; then
        echo "==> [5.5/6] Notarizing .app bundle with Apple notarytool..."
        NOTARY_TMP_ZIP="${DIST_DIR}/notary-${APP_NAME}.zip"
        rm -f "${NOTARY_TMP_ZIP}"
        ditto -c -k --keepParent "${APP_BUNDLE}" "${NOTARY_TMP_ZIP}"
        xcrun notarytool submit "${NOTARY_TMP_ZIP}" "${NOTARY_AUTH_ARGS[@]}" --wait
        rm -f "${NOTARY_TMP_ZIP}"
        xcrun stapler staple "${APP_BUNDLE}"
        xcrun stapler validate "${APP_BUNDLE}"
        spctl -a -vvv -t exec "${APP_BUNDLE}"
    elif [ "${REQUIRE_APPLE_NOTARIZATION:-0}" = "1" ]; then
        echo "Error: Apple notarization credentials are required. Set APPLE_NOTARY_PROFILE or APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD." >&2
        exit 1
    else
        echo "==> [5.5/6] Skipping Apple notarization (set APPLE_NOTARY_PROFILE or APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD to enable)."
    fi
fi

echo "==> [6/6] Generating distribution packages (DMG & ZIP)..."
# 1. Zip
cd "${DIST_DIR}"
ZIP_NAME="${APP_NAME}-${VERSION}-${ARCH}.zip"
zip -r -y -q "${ZIP_NAME}" "${APP_NAME}.app"

# 2. DMG
DMG_NAME="${APP_NAME}-${VERSION}-${ARCH}.dmg"
DMG_TEMP="${DIST_DIR}/${APP_NAME}-temp.dmg"
DMG_STAGING="${DIST_DIR}/dmg-staging"
DMG_BG="${DESKTOP_DIR}/build/dmg-background.png"
rm -rf "${DMG_STAGING}" "${DMG_TEMP}"
mkdir -p "${DMG_STAGING}"
cp -R "${APP_BUNDLE}" "${DMG_STAGING}/"
ln -s /Applications "${DMG_STAGING}/Applications"

if [ -f "${DMG_BG}" ]; then
    mkdir -p "${DMG_STAGING}/.background"
    cp "${DMG_BG}" "${DMG_STAGING}/.background/dmg-background.png"

    hdiutil create -volname "${APP_NAME}" -srcfolder "${DMG_STAGING}" -ov -fs HFS+ -format UDRW "${DMG_TEMP}"
    MOUNT_DIR="/Volumes/${APP_NAME}"
    # Detach any stale mount with the same volume name first
    if [ -d "${MOUNT_DIR}" ]; then
        hdiutil detach "${MOUNT_DIR}" -force >/dev/null 2>&1 || true
    fi
    DEVICE=$(hdiutil attach -readwrite -noverify -noautoopen "${DMG_TEMP}" | awk '/Apple_HFS/ {print $1; exit}')
    sleep 1
    if [ -n "${DEVICE}" ] && [ -d "${MOUNT_DIR}" ]; then
        osascript <<EOF || true
tell application "Finder"
    tell disk "${APP_NAME}"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set the bounds of container window to {200, 120, 880, 560}
        set viewOptions to the icon view options of container window
        set arrangement of viewOptions to not arranged
        set icon size of viewOptions to 96
        set text size of viewOptions to 13
        set background picture of viewOptions to file ".background:dmg-background.png"
        set position of item "${APP_NAME}.app" of container window to {185, 225}
        set position of item "Applications" of container window to {495, 225}
        update without registering applications
        delay 2
        close
    end tell
end tell
EOF
        sync
        hdiutil detach "${DEVICE}" -quiet || hdiutil detach "${DEVICE}" -force || true
    fi
    hdiutil convert "${DMG_TEMP}" -format UDZO -imagekey zlib-level=9 -o "${DMG_NAME}"
    rm -f "${DMG_TEMP}"
else
    hdiutil create -volname "${APP_NAME}" -srcfolder "${DMG_STAGING}" -ov -format UDZO "${DMG_NAME}"
fi
rm -rf "${DMG_STAGING}"

if [ -n "${SIGN_IDENTITY:-}" ] && command -v codesign &>/dev/null; then
    echo "==> Signing DMG with '${SIGN_IDENTITY}'..."
    codesign --force --timestamp --sign "${SIGN_IDENTITY}" "${DMG_NAME}"
    codesign --verify --strict --verbose=2 "${DMG_NAME}"
    if [ ${#NOTARY_AUTH_ARGS[@]} -gt 0 ]; then
        echo "==> Notarizing DMG..."
        xcrun notarytool submit "${DMG_NAME}" "${NOTARY_AUTH_ARGS[@]}" --wait
        xcrun stapler staple "${DMG_NAME}"
        xcrun stapler validate "${DMG_NAME}"
    fi
fi

echo "==> Generating latest.json update manifest..."
node "${DESKTOP_DIR}/scripts/generate-manifest.mjs"

echo "=========================================================="
echo " Packaging completed successfully!"
echo " Artifacts generated in: ${DIST_DIR}"
echo "   - App Bundle: ${APP_BUNDLE}"
echo "   - DMG:        ${DIST_DIR}/${DMG_NAME}"
echo "   - ZIP:        ${DIST_DIR}/${ZIP_NAME}"
echo "=========================================================="
