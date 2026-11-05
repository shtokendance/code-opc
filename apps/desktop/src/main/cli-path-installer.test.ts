// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { lstat, mkdir, mkdtemp, readlink, rm, writeFile } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import {
  checkCliOnPath,
  defaultBinaryName,
  defaultCandidateDirs,
  ensureCliInPath,
  ensureCliInPathWithDeps,
} from "./cli-path-installer";

describe("cli-path-installer", () => {
  let tempDir: string;
  let dummyBundledBinary: string;

  beforeEach(async () => {
    tempDir = await mkdtemp(join(tmpdir(), "multica-cli-path-test-"));
    dummyBundledBinary = join(tempDir, "bundled-multica");
    await writeFile(dummyBundledBinary, "#!/bin/sh\necho multica\n", { mode: 0o755 });
  });

  afterEach(async () => {
    await rm(tempDir, { recursive: true, force: true });
  });

  describe("defaultBinaryName", () => {
    it("returns multica.exe on Windows and multica on other platforms", () => {
      const expected = process.platform === "win32" ? "multica.exe" : "multica";
      expect(defaultBinaryName()).toBe(expected);
    });
  });

  describe("defaultCandidateDirs", () => {
    it("returns default candidate directories for PATH installation", () => {
      const dirs = defaultCandidateDirs();
      expect(Array.isArray(dirs)).toBe(true);
      expect(dirs.length).toBeGreaterThan(0);
      if (process.platform !== "win32") {
        expect(dirs).toContain(join(homedir(), ".local", "bin"));
        expect(dirs).toContain("/usr/local/bin");
      }
    });
  });

  describe("checkCliOnPath", () => {
    it("returns a boolean indicating presence on PATH", async () => {
      const result = await checkCliOnPath("non_existent_binary_xyz_12345");
      expect(result).toBe(false);
    });
  });

  describe("ensureCliInPathWithDeps", () => {
    it("does not install and returns already_on_path when CLI is already on PATH", async () => {
      const targetDir = join(tempDir, "bin");
      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: dummyBundledBinary,
        targetDirs: [targetDir],
        isAvailableOnPath: async () => true,
      });

      expect(result).toEqual({
        installed: false,
        reason: "already_on_path",
      });
    });

    it("returns no_bundled_binary when bundled binary does not exist", async () => {
      const nonExistentBinary = join(tempDir, "missing-binary");
      const targetDir = join(tempDir, "bin");

      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: nonExistentBinary,
        targetDirs: [targetDir],
        isAvailableOnPath: async () => false,
      });

      expect(result).toEqual({
        installed: false,
        reason: "no_bundled_binary",
      });
    });

    it("installs binary into target directory when not on PATH and bundled binary exists", async () => {
      const targetDir = join(tempDir, "bin");
      const binName = defaultBinaryName();
      const expectedTargetPath = join(targetDir, binName);

      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: dummyBundledBinary,
        targetDirs: [targetDir],
        isAvailableOnPath: async () => false,
      });

      expect(result).toEqual({
        installed: true,
        targetPath: expectedTargetPath,
        reason: "installed",
      });

      const statInfo = await lstat(expectedTargetPath);
      if (process.platform === "win32") {
        expect(statInfo.isFile()).toBe(true);
      } else {
        expect(statInfo.isSymbolicLink()).toBe(true);
        const linkTarget = await readlink(expectedTargetPath);
        expect(linkTarget).toBe(dummyBundledBinary);
      }
    });

    it("replaces existing destination file or symlink cleanly", async () => {
      const targetDir = join(tempDir, "bin");
      await mkdir(targetDir, { recursive: true });
      const binName = defaultBinaryName();
      const targetPath = join(targetDir, binName);
      await writeFile(targetPath, "old-content");

      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: dummyBundledBinary,
        targetDirs: [targetDir],
        isAvailableOnPath: async () => false,
      });

      expect(result.installed).toBe(true);
      expect(result.targetPath).toBe(targetPath);
      const statInfo = await lstat(targetPath);
      if (process.platform !== "win32") {
        expect(statInfo.isSymbolicLink()).toBe(true);
      }
    });

    it("gracefully falls back to next candidate directory if first fails", async () => {
      // Create a file where the first candidate directory would be, causing mkdir to fail with ENOTDIR
      const failingDir = join(tempDir, "failing-dir-file");
      await writeFile(failingDir, "blocking-file");
      const fallbackDir = join(tempDir, "fallback-bin");
      const binName = defaultBinaryName();
      const expectedTargetPath = join(fallbackDir, binName);

      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: dummyBundledBinary,
        targetDirs: [failingDir, fallbackDir],
        isAvailableOnPath: async () => false,
      });

      expect(result).toEqual({
        installed: true,
        targetPath: expectedTargetPath,
        reason: "installed",
      });

      const statInfo = await lstat(expectedTargetPath);
      expect(statInfo).toBeDefined();
    });

    it("returns all_targets_failed if all candidate directories fail", async () => {
      const failingDir1 = join(tempDir, "fail1");
      const failingDir2 = join(tempDir, "fail2");
      await writeFile(failingDir1, "blocking-file-1");
      await writeFile(failingDir2, "blocking-file-2");

      const result = await ensureCliInPathWithDeps({
        bundledBinaryPath: dummyBundledBinary,
        targetDirs: [failingDir1, failingDir2],
        isAvailableOnPath: async () => false,
      });

      expect(result).toEqual({
        installed: false,
        reason: "all_targets_failed",
      });
    });
  });

  describe("ensureCliInPath", () => {
    it("delegates to ensureCliInPathWithDeps with default options", async () => {
      const nonExistentBinary = join(tempDir, "non-existent-multica");
      const result = await ensureCliInPath(nonExistentBinary);
      expect(result.installed).toBe(false);
    });
  });
});
