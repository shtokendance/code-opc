#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${DESKTOP_DIR}/dist"
APP_NAME="Multica"
VERSION="0.9.0"
GOARCH="${1:-amd64}"
ARCH_LABEL="x64"
if [ "${GOARCH}" = "arm64" ]; then
    ARCH_LABEL="arm64"
fi

echo "==> [1/4] Building frontend assets..."
cd "${DESKTOP_DIR}"
pnpm exec electron-vite build

echo "==> [2/4] Preparing Windows distribution directory..."
WIN_STAGING="${DIST_DIR}/${APP_NAME}-${VERSION}-win-${ARCH_LABEL}"
ZIP_NAME="${APP_NAME}-${VERSION}-win-${ARCH_LABEL}.zip"
rm -rf "${WIN_STAGING}" "${DIST_DIR}/${ZIP_NAME}"
mkdir -p "${WIN_STAGING}"

echo "==> [3/4] Cross-compiling native Windows binary (${GOARCH}, GUI subsystem)..."
GOOS=windows GOARCH="${GOARCH}" CGO_ENABLED=0 go build -trimpath -ldflags="-H=windowsgui -s -w" -o "${WIN_STAGING}/${APP_NAME}.exe" .

echo "==> [4/4] Creating Windows distribution archive (${ZIP_NAME})..."
cd "${DIST_DIR}"
zip -r -q "${ZIP_NAME}" "${APP_NAME}-${VERSION}-win-${ARCH_LABEL}"

echo "=========================================================="
echo " Windows packaging completed successfully!"
echo " Artifacts generated in: ${DIST_DIR}"
echo "   - Executable: ${WIN_STAGING}/${APP_NAME}.exe"
echo "   - ZIP:        ${DIST_DIR}/${ZIP_NAME}"
echo "=========================================================="
