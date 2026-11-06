import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { chmod, copyFile, mkdir, rm, symlink } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";

export interface PathInstallResult {
  installed: boolean;
  targetPath?: string;
  reason: "already_on_path" | "installed" | "no_bundled_binary" | "all_targets_failed";
  error?: string;
}

export function defaultBinaryName(): string {
  return process.platform === "win32" ? "multica.exe" : "multica";
}

export function defaultCandidateDirs(): string[] {
  const dirs: string[] = [];
  if (process.platform !== "win32") {
    if (process.platform === "darwin" && existsSync("/opt/homebrew/bin")) {
      dirs.push("/opt/homebrew/bin");
    }
    dirs.push(join(homedir(), ".local", "bin"));
    dirs.push("/usr/local/bin");
  } else {
    dirs.push(
      join(
        process.env.LOCALAPPDATA || join(homedir(), "AppData", "Local"),
        "Microsoft",
        "WindowsApps",
      ),
    );
  }
  return dirs;
}

export function checkCliOnPath(
  binName: string = defaultBinaryName(),
): Promise<boolean> {
  return new Promise((resolve) => {
    try {
      const cmd = process.platform === "win32" ? "where.exe" : "which";
      execFile(cmd, [binName], { timeout: 5_000 }, (err) => {
        resolve(!err);
      });
    } catch {
      resolve(false);
    }
  });
}

export interface EnsureCliInPathDeps {
  bundledBinaryPath: string;
  targetDirs?: string[];
  isAvailableOnPath?: () => Promise<boolean>;
}

export async function ensureCliInPathWithDeps(
  deps: EnsureCliInPathDeps,
): Promise<PathInstallResult> {
  const isAvailable = deps.isAvailableOnPath ?? checkCliOnPath;
  if (await isAvailable()) {
    return { installed: false, reason: "already_on_path" };
  }

  if (!existsSync(deps.bundledBinaryPath)) {
    return { installed: false, reason: "no_bundled_binary" };
  }

  const targets = deps.targetDirs ?? defaultCandidateDirs();
  const binName = defaultBinaryName();
  const sourcePath = resolve(deps.bundledBinaryPath);
  let lastError: string | undefined;

  for (const dir of targets) {
    const dest = join(dir, binName);
    try {
      await mkdir(dir, { recursive: true });
      await rm(dest, { force: true, recursive: true }).catch(() => {});
      if (process.platform === "win32") {
        await copyFile(sourcePath, dest);
      } else {
        await symlink(sourcePath, dest);
        await chmod(dest, 0o755).catch(() => {});
      }
      return { installed: true, targetPath: dest, reason: "installed" };
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      // Try next directory if permission denied (e.g. /usr/local/bin without sudo or ENOTDIR)
      continue;
    }
  }

  return { installed: false, reason: "all_targets_failed", error: lastError };
}

export async function ensureCliInPath(
  bundledBinaryPath: string,
): Promise<PathInstallResult> {
  return ensureCliInPathWithDeps({ bundledBinaryPath });
}
