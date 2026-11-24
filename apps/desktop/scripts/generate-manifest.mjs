#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const desktopDir = path.resolve(__dirname, "..");
const distDir = path.join(desktopDir, "dist");
const pkg = JSON.parse(fs.readFileSync(path.join(desktopDir, "package.json"), "utf8"));
const version = pkg.version;
const productName = pkg.productName || "Multica";

if (!fs.existsSync(distDir)) {
  fs.mkdirSync(distDir, { recursive: true });
}

function getSha256(filePath) {
  const data = fs.readFileSync(filePath);
  return crypto.createHash("sha256").update(data).digest("hex");
}

const manifestPath = path.join(distDir, "latest.json");
let manifest = {
  version,
  release_notes: `新版本 v${version} 已发布，包含多项性能与体验优化。`,
  pub_date: new Date().toISOString(),
  platforms: {},
};

if (fs.existsSync(manifestPath)) {
  try {
    const existing = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
    if (existing.version === version && existing.platforms) {
      manifest = {
        ...existing,
        release_notes: `新版本 v${version} 已发布，包含多项性能与体验优化。`,
        pub_date: new Date().toISOString(),
      };
    }
  } catch {}
}

const files = fs.readdirSync(distDir);
for (const file of files) {
  // Only register zip files that match current version
  if (!file.endsWith(".zip") || !file.includes(version)) continue;
  const filePath = path.join(distDir, file);
  const stat = fs.statSync(filePath);
  const sha256 = getSha256(filePath);

  // Match platform
  if (file.includes("win-x64") || file.includes("win_amd64") || file.includes("windows-amd64")) {
    manifest.platforms["windows-amd64"] = { filename: file, sha256, size: stat.size };
    manifest.platforms["windows-x64"] = { filename: file, sha256, size: stat.size };
  } else if (file.includes("win-arm64") || file.includes("windows-arm64")) {
    manifest.platforms["windows-arm64"] = { filename: file, sha256, size: stat.size };
  } else if (file.includes("arm64") && (file.includes("mac") || !file.includes("win"))) {
    manifest.platforms["darwin-arm64"] = { filename: file, sha256, size: stat.size };
    manifest.platforms["macos-arm64"] = { filename: file, sha256, size: stat.size };
  } else if ((file.includes("x64") || file.includes("amd64")) && (file.includes("mac") || !file.includes("win"))) {
    manifest.platforms["darwin-amd64"] = { filename: file, sha256, size: stat.size };
    manifest.platforms["macos-x64"] = { filename: file, sha256, size: stat.size };
  }
}

manifest.version = version;
manifest.pub_date = new Date().toISOString();

fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n");
const prodManifestPath = path.join(distDir, `latest-${productName.toLowerCase()}.json`);
fs.writeFileSync(prodManifestPath, JSON.stringify(manifest, null, 2) + "\n");

console.log(`Generated update manifests:\n  - ${manifestPath}\n  - ${prodManifestPath}`);
