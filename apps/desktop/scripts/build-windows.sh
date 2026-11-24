#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${DESKTOP_DIR}/dist"
APP_NAME="CodeOPC"
VERSION="$(node -p "require('${DESKTOP_DIR}/package.json').version")"
GOARCH="${1:-amd64}"
ARCH_LABEL="x64"
if [ "${GOARCH}" = "arm64" ]; then
    ARCH_LABEL="arm64"
fi

echo "==> [1/5] Building frontend assets..."
cd "${DESKTOP_DIR}"
pnpm exec electron-vite build

echo "==> [2/5] Compiling Windows PE resources (manifest, icons, version)..."
cd "${DESKTOP_DIR}"
go run github.com/tc-hib/go-winres@latest make

echo "==> [3/5] Preparing Windows distribution directory..."
WIN_STAGING="${DIST_DIR}/${APP_NAME}-${VERSION}-win-${ARCH_LABEL}"
ZIP_NAME="${APP_NAME}-${VERSION}-win-${ARCH_LABEL}.zip"
rm -rf "${WIN_STAGING}" "${DIST_DIR}/${ZIP_NAME}"
mkdir -p "${WIN_STAGING}"

echo "==> [4/5] Cross-compiling native Windows binary (${GOARCH}, GUI subsystem)..."
GOOS=windows GOARCH="${GOARCH}" CGO_ENABLED=0 go build -trimpath -ldflags="-H=windowsgui -s -w -X main.appVersion=${VERSION} -X main.appProductName=${APP_NAME}" -o "${WIN_STAGING}/${APP_NAME}.exe" .
cp "${DESKTOP_DIR}/build/icon.ico" "${WIN_STAGING}/${APP_NAME}.ico"

echo "==> [5/5] Creating Windows distribution archive (${ZIP_NAME})..."
cd "${DIST_DIR}"
zip -r -q "${ZIP_NAME}" "${APP_NAME}-${VERSION}-win-${ARCH_LABEL}"

echo "==> Generating latest.json update manifest..."
node "${DESKTOP_DIR}/scripts/generate-manifest.mjs"

echo "=========================================================="
echo " Windows packaging completed successfully!"
echo " Artifacts generated in: ${DIST_DIR}"
echo "   - Executable: ${WIN_STAGING}/${APP_NAME}.exe"
echo "   - Icon:       ${WIN_STAGING}/${APP_NAME}.ico"
echo "   - ZIP:        ${DIST_DIR}/${ZIP_NAME}"
echo "=========================================================="
