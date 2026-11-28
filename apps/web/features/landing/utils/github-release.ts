import type { DownloadAssets, DownloadSizes } from "./parse-release-assets";

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
  sizes: DownloadSizes;
}

const REVALIDATE_SECONDS = 300;
export const RELEASE_TIMEOUT_MS = 3000;

interface DesktopUpdateCheckPayload {
  available?: boolean;
  latest_version?: string;
  pub_date?: string;
  download_url?: string;
  filename?: string;
  size?: number;
}

function getReleaseApiBase(): string {
  return (
    process.env.INTERNAL_API_URL ||
    process.env.NEXT_PUBLIC_API_URL ||
    "https://code-opc.benlai.com"
  ).replace(/\/+$/, "");
}

async function fetchPlatformRelease(
  platform: "darwin" | "windows",
  arch: "arm64" | "amd64",
  signal: AbortSignal,
): Promise<DesktopUpdateCheckPayload | null> {
  const url = `${getReleaseApiBase()}/api/desktop/updates/check?platform=${platform}&arch=${arch}&version=0.0.1&product=CodeOPC`;
  const res = await fetch(url, {
    next: { revalidate: REVALIDATE_SECONDS },
    signal,
  });
  if (!res.ok) return null;
  return (await res.json()) as DesktopUpdateCheckPayload;
}

export async function fetchLatestRelease(): Promise<LatestRelease> {
  const controller = new AbortController();
  const timer = setTimeout(
    () => controller.abort(new Error(`Desktop release check timed out after ${RELEASE_TIMEOUT_MS}ms`)),
    RELEASE_TIMEOUT_MS,
  );
  try {
    const [mac, win] = await Promise.all([
      fetchPlatformRelease("darwin", "arm64", controller.signal),
      fetchPlatformRelease("windows", "amd64", controller.signal),
    ]);

    const assets: DownloadAssets = {};
    const sizes: DownloadSizes = {};

    if (mac?.download_url) {
      const isDmg = (mac.filename ?? mac.download_url).toLowerCase().endsWith(".dmg");
      const key = isDmg ? "macArm64Dmg" : "macArm64Zip";
      assets[key] = mac.download_url;
      if (mac.size) sizes[key] = mac.size;
    }

    if (win?.download_url) {
      const isExe = (win.filename ?? win.download_url).toLowerCase().endsWith(".exe");
      const key = isExe ? "winX64Exe" : "winX64Zip";
      assets[key] = win.download_url;
      if (win.size) sizes[key] = win.size;
    }

    return {
      version: mac?.latest_version ?? win?.latest_version ?? null,
      publishedAt: mac?.pub_date ?? win?.pub_date ?? null,
      htmlUrl: null,
      assets,
      sizes,
    };
  } catch (err) {
    console.warn("[download] fetchLatestRelease failed:", err);
    return emptyRelease();
  } finally {
    clearTimeout(timer);
  }
}

function emptyRelease(): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    assets: {},
    sizes: {},
  };
}
