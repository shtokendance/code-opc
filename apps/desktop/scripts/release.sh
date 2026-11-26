#!/bin/bash
set -euo pipefail

# Unified release script for CodeOPC desktop (test / prod / both).
#
# Usage:
#   bash scripts/release.sh --env test|prod|both [--version X.Y.Z] [--upload]
#
# Examples:
#   bash scripts/release.sh --env test --version 0.11.23 --upload
#   bash scripts/release.sh --env prod --version 0.11.23
#   bash scripts/release.sh --env both --version 0.11.23 --upload

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${DESKTOP_DIR}/../.." && pwd)"
DIST_DIR="${DESKTOP_DIR}/dist"

TEST_SERVER="${TEST_SERVER:-ecs-user@10.101.102.116}"
TEST_RELEASES_DIR="${TEST_RELEASES_DIR:-/data/releases}"
TEST_API_BASE="${TEST_API_BASE:-http://10.101.102.116:3088}"

TARGET_ENV=""
VERSION=""
UPLOAD=0

usage() {
    cat <<EOF
Usage: $(basename "$0") --env <test|prod|both> [options]

Options:
  --env <test|prod|both>   Release target environment (required)
  --version <X.Y.Z>        Bump desktop version before building (optional; defaults to package.json version)
  --upload                 Upload build artifacts to server (currently uploads test builds to test server; prod ignored)
  -h, --help               Show this help
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --env)
            TARGET_ENV="${2:-}"
            shift 2
            ;;
        --version)
            VERSION="${2:-}"
            shift 2
            ;;
        --upload)
            UPLOAD=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            usage
            exit 1
            ;;
    esac
done

if [[ "${TARGET_ENV}" != "test" && "${TARGET_ENV}" != "prod" && "${TARGET_ENV}" != "both" ]]; then
    echo "Error: --env must be one of: test, prod, both" >&2
    usage
    exit 1
fi

ensure_apple_signing_cert() {
    local identity
    identity="$(security find-identity -v -p codesigning 2>/dev/null | sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -n 1 || true)"

    if [[ -z "${identity}" ]]; then
        local p12_path="${APPLE_SIGNING_P12:-}"
        local ca_path="${APPLE_SIGNING_CA:-}"
        local tmp_dir=""

        if [[ -z "${p12_path}" && -f "${REPO_ROOT}/apple-signing/developer-id-full.p12" ]]; then
            p12_path="${REPO_ROOT}/apple-signing/developer-id-full.p12"
            ca_path="${REPO_ROOT}/apple-signing/DeveloperIDG2CA.cer"
        elif [[ -z "${p12_path}" && -f "${REPO_ROOT}/apple-signing.zip" ]]; then
            tmp_dir="$(mktemp -d)"
            unzip -q -o "${REPO_ROOT}/apple-signing.zip" -d "${tmp_dir}"
            p12_path="${tmp_dir}/apple-signing/developer-id-full.p12"
            ca_path="${tmp_dir}/apple-signing/DeveloperIDG2CA.cer"
        fi

        if [[ -n "${p12_path}" && -f "${p12_path}" ]]; then
            local p12_pass="${APPLE_SIGNING_PASSWORD:-}"
            if [[ -z "${p12_pass}" && -t 0 ]]; then
                read -r -s -p "Enter password for ${p12_path}: " p12_pass
                echo ""
            fi
            if [[ -z "${p12_pass}" ]]; then
                [[ -n "${tmp_dir}" ]] && rm -rf "${tmp_dir}"
                echo "Error: APPLE_SIGNING_PASSWORD is required to import ${p12_path}." >&2
                exit 1
            fi
            if [[ -n "${ca_path}" && -f "${ca_path}" ]]; then
                security import "${ca_path}" -k "${HOME}/Library/Keychains/login.keychain-db" >/dev/null 2>&1 || true
            fi
            security import "${p12_path}" -k "${HOME}/Library/Keychains/login.keychain-db" -P "${p12_pass}" -T /usr/bin/codesign
        fi

        [[ -n "${tmp_dir}" ]] && rm -rf "${tmp_dir}"
        identity="$(security find-identity -v -p codesigning 2>/dev/null | sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -n 1 || true)"
    fi

    if [[ -z "${identity}" ]]; then
        echo "Error: No 'Developer ID Application' certificate found in keychain. Cannot build signed production release." >&2
        exit 1
    fi

    export APPLE_SIGNING_IDENTITY="${identity}"
    echo "==> Verified Apple signing identity: ${APPLE_SIGNING_IDENTITY}"
}

verify_signed_app() {
    local app_path="${DIST_DIR}/CodeOPC.app"
    local require_dev_id="${1:-0}"

    codesign --verify --deep --strict "${app_path}"
    if [[ "${require_dev_id}" == "1" ]]; then
        local details
        details="$(codesign -dvv "${app_path}" 2>&1)"
        if ! echo "${details}" | grep -q "Authority=Developer ID Application:"; then
            echo "Error: ${app_path} is NOT signed with a Developer ID Application certificate!" >&2
            echo "${details}" >&2
            exit 1
        fi
        echo "==> Confirmed Developer ID signature & hardened runtime on ${app_path}"
    fi
}

bump_version() {
    local next_version="$1"
    if [[ ! "${next_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Error: --version must match X.Y.Z (got '${next_version}')" >&2
        exit 1
    fi
    echo "==> Bumping desktop version to ${next_version}..."
    node -e '
      const fs = require("fs");
      const version = process.argv[1];
      const pkgPath = process.argv[2];
      const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
      pkg.version = version;
      fs.writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");

      const appGoPath = process.argv[3];
      let appGo = fs.readFileSync(appGoPath, "utf8");
      appGo = appGo.replace(/appVersion\s*=\s*"[^"]+"/, `appVersion     = "${version}"`);
      fs.writeFileSync(appGoPath, appGo);

      const wailsApiPath = process.argv[4];
      let wailsApi = fs.readFileSync(wailsApiPath, "utf8");
      wailsApi = wailsApi.replace(/version:\s*"[^"]+"/, `version: "${version}"`);
      fs.writeFileSync(wailsApiPath, wailsApi);
    ' "${next_version}" "${DESKTOP_DIR}/package.json" "${DESKTOP_DIR}/app.go" "${DESKTOP_DIR}/src/renderer/src/platform/wails-desktop-api.ts"
}

if [[ -n "${VERSION}" ]]; then
    bump_version "${VERSION}"
else
    VERSION="$(node -p "require('${DESKTOP_DIR}/package.json').version")"
fi

echo "==> Target version: ${VERSION} | Env: ${TARGET_ENV}"

if [[ "${TARGET_ENV}" == "prod" || "${TARGET_ENV}" == "both" ]]; then
    ensure_apple_signing_cert
fi

if [[ "${TARGET_ENV}" == "test" || "${TARGET_ENV}" == "both" ]]; then
    echo "=========================================================="
    echo " Building TEST release (v${VERSION}-test)"
    echo "=========================================================="

    cd "${DESKTOP_DIR}"
    DESKTOP_ENV=test pnpm run package:all-native
    DESKTOP_ENV=test pnpm run package:manifest
    verify_signed_app 0

    if [[ "${UPLOAD}" == "1" ]]; then
        echo "==> Uploading test artifacts to ${TEST_SERVER}:${TEST_RELEASES_DIR}/..."
        scp \
            "${DIST_DIR}/CodeOPC-${VERSION}-arm64.dmg" \
            "${DIST_DIR}/CodeOPC-${VERSION}-arm64.zip" \
            "${DIST_DIR}/CodeOPC-${VERSION}-win-x64.zip" \
            "${DIST_DIR}/latest.json" \
            "${DIST_DIR}/latest-codeopc.json" \
            "${TEST_SERVER}:${TEST_RELEASES_DIR}/"
        ssh "${TEST_SERVER}" "cp ${TEST_RELEASES_DIR}/latest-codeopc.json ${TEST_RELEASES_DIR}/latest-multica.json"

        echo "==> Verifying test server update API..."
        curl -fsS "${TEST_API_BASE}/api/desktop/updates/check?platform=darwin&arch=arm64&version=0.0.1&product=Multica"
        echo ""
        curl -fsS "${TEST_API_BASE}/api/desktop/updates/check?platform=windows&arch=amd64&version=0.0.1&product=Multica"
        echo ""
    fi
fi

if [[ "${TARGET_ENV}" == "prod" || "${TARGET_ENV}" == "both" ]]; then
    echo "=========================================================="
    echo " Building PRODUCTION release (v${VERSION})"
    echo "=========================================================="

    cd "${DESKTOP_DIR}"
    DESKTOP_ENV=prod REQUIRE_APPLE_SIGNING=1 pnpm run package:all-native
    DESKTOP_ENV=prod pnpm run package:manifest
    verify_signed_app 1

    PROD_OUT_DIR="${DIST_DIR}/release"
    echo "==> Copying production artifacts to ${PROD_OUT_DIR}/..."
    rm -rf "${PROD_OUT_DIR}"
    mkdir -p "${PROD_OUT_DIR}"
    cp -R "${DIST_DIR}/CodeOPC.app" "${PROD_OUT_DIR}/"
    cp -R "${DIST_DIR}/CodeOPC-${VERSION}-win-x64" "${PROD_OUT_DIR}/"
    cp \
        "${DIST_DIR}/CodeOPC-${VERSION}-arm64.dmg" \
        "${DIST_DIR}/CodeOPC-${VERSION}-arm64.zip" \
        "${DIST_DIR}/CodeOPC-${VERSION}-win-x64.zip" \
        "${DIST_DIR}/latest.json" \
        "${DIST_DIR}/latest-codeopc.json" \
        "${PROD_OUT_DIR}/"
fi

echo "=========================================================="
echo " Release v${VERSION} (${TARGET_ENV}) completed!"
if [[ "${TARGET_ENV}" == "prod" || "${TARGET_ENV}" == "both" ]]; then
    echo " Production artifacts in ${DIST_DIR}/release:"
    ls -lh "${DIST_DIR}/release/CodeOPC-${VERSION}"* "${DIST_DIR}/release/latest"*.json
else
    echo " Artifacts in ${DIST_DIR}:"
    ls -lh "${DIST_DIR}/CodeOPC-${VERSION}"* "${DIST_DIR}/latest"*.json
fi
echo "=========================================================="
