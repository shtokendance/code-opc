// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import {
  defaultCliConfigPath,
  syncCliSelfHostConfig,
  syncCliSelfHostConfigPath,
  type SelfHostUrls,
} from "./cli-config-sync";

const tempDirs: string[] = [];

async function makeTempConfigPath(filename = "config.json"): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "multica-cli-sync-test-"));
  tempDirs.push(dir);
  return join(dir, filename);
}

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })),
  );
});

describe("cli-config-sync", () => {
  describe("defaultCliConfigPath", () => {
    it("returns join(homedir(), '.multica', 'config.json')", () => {
      expect(defaultCliConfigPath()).toBe(join(homedir(), ".multica", "config.json"));
    });
  });

  describe("syncCliSelfHostConfigPath", () => {
    it("creates non-existent config.json and writes server_url and app_url", async () => {
      const configPath = await makeTempConfigPath("nested/dir/config.json");
      const urls: SelfHostUrls = {
        apiUrl: "http://localhost:8080",
        appUrl: "http://localhost:3000",
      };

      const result = await syncCliSelfHostConfigPath(configPath, urls);

      expect(result).toBe(true);
      const rawContent = await readFile(configPath, "utf-8");
      expect(rawContent.endsWith("\n")).toBe(true);
      expect(JSON.parse(rawContent)).toEqual({
        server_url: "http://localhost:8080",
        app_url: "http://localhost:3000",
      });
    });

    it("preserves existing fields while updating server_url and app_url", async () => {
      const configPath = await makeTempConfigPath();
      const existingConfig = {
        server_url: "http://old.example.com",
        app_url: "http://old-app.example.com",
        device_name: "test-device",
        max_concurrent_tasks: 4,
      };
      await writeFile(configPath, JSON.stringify(existingConfig, null, 2) + "\n", "utf-8");

      const urls: SelfHostUrls = {
        apiUrl: "http://new.example.com",
        appUrl: "http://new-app.example.com",
      };

      const result = await syncCliSelfHostConfigPath(configPath, urls);

      expect(result).toBe(true);
      const parsed = JSON.parse(await readFile(configPath, "utf-8"));
      expect(parsed).toEqual({
        server_url: "http://new.example.com",
        app_url: "http://new-app.example.com",
        device_name: "test-device",
        max_concurrent_tasks: 4,
      });
    });

    it("returns false and does not rewrite disk when server_url and app_url are already up-to-date", async () => {
      const configPath = await makeTempConfigPath();
      const currentConfig = {
        server_url: "http://same.example.com",
        app_url: "http://same-app.example.com",
        device_name: "preserved-device",
      };
      await writeFile(configPath, JSON.stringify(currentConfig, null, 2) + "\n", "utf-8");

      const beforeStat = await stat(configPath);

      const urls: SelfHostUrls = {
        apiUrl: "http://same.example.com",
        appUrl: "http://same-app.example.com",
      };

      const result = await syncCliSelfHostConfigPath(configPath, urls);

      expect(result).toBe(false);
      const afterStat = await stat(configPath);
      expect(afterStat.mtimeMs).toBe(beforeStat.mtimeMs);
    });

    it("handles corrupted JSON gracefully and writes new urls", async () => {
      const configPath = await makeTempConfigPath();
      await writeFile(configPath, "{ invalid json content", "utf-8");

      const urls: SelfHostUrls = {
        apiUrl: "http://recovered.example.com",
        appUrl: "http://recovered-app.example.com",
      };

      const result = await syncCliSelfHostConfigPath(configPath, urls);

      expect(result).toBe(true);
      const parsed = JSON.parse(await readFile(configPath, "utf-8"));
      expect(parsed).toEqual({
        server_url: "http://recovered.example.com",
        app_url: "http://recovered-app.example.com",
      });
    });
  });

  describe("syncCliSelfHostConfig", () => {
    it("delegates to syncCliSelfHostConfigPath with target config path", async () => {
      const configPath = await makeTempConfigPath();
      const urls: SelfHostUrls = {
        apiUrl: "http://cli.selfhost.test:8080",
        appUrl: "http://cli.selfhost.test:3000",
      };

      const result = await syncCliSelfHostConfig(urls, configPath);
      expect(result).toBe(true);

      const parsed = JSON.parse(await readFile(configPath, "utf-8"));
      expect(parsed).toEqual({
        server_url: "http://cli.selfhost.test:8080",
        app_url: "http://cli.selfhost.test:3000",
      });
    });
  });
});
