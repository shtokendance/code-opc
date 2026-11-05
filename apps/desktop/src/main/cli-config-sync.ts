import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

export interface SelfHostUrls {
  apiUrl: string;
  appUrl: string;
}

export function defaultCliConfigPath(): string {
  return join(homedir(), ".multica", "config.json");
}

export async function syncCliSelfHostConfigPath(
  configPath: string,
  urls: SelfHostUrls,
): Promise<boolean> {
  let existing: Record<string, unknown> = {};

  try {
    const raw = await readFile(configPath, "utf-8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      existing = parsed as Record<string, unknown>;
    }
  } catch {
    existing = {};
  }

  if (existing.server_url === urls.apiUrl && existing.app_url === urls.appUrl) {
    return false;
  }

  const updated: Record<string, unknown> = {
    ...existing,
    server_url: urls.apiUrl,
    app_url: urls.appUrl,
  };

  const formatted = JSON.stringify(updated, null, 2) + "\n";
  const parentDir = dirname(configPath);
  await mkdir(parentDir, { recursive: true });

  const tempPath = `${configPath}.tmp-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  await writeFile(tempPath, formatted, "utf-8");
  await rename(tempPath, configPath);

  return true;
}

export async function syncCliSelfHostConfig(
  urls: SelfHostUrls,
  configPath: string = defaultCliConfigPath(),
): Promise<boolean> {
  return syncCliSelfHostConfigPath(configPath, urls);
}
