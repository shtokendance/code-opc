# Desktop CLI Auto-Install and Self-Host Auto-Configure Implementation Plan

> **针对智能体工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务实施此计划。步骤使用复选框 (`- [ ]`) 语法进行追踪。

**目标：** 在 code-opc 桌面端启动时，自动判断 multica CLI 是否安装在系统 PATH 中；未安装则自动安装/软链到 PATH，自动将当前环境的服务端与应用地址同步至 CLI 配置，并在后台启动守护进程（daemon）。

**架构：** 在 Electron 主进程（`apps/desktop/src/main`）启动生命周期内新增两个高内聚模块：`cli-config-sync.ts`（负责原子写入 `~/.multica/config.json` 保证 self-host 配置同步）和 `cli-path-installer.ts`（负责探测并在 PATH 缺失时从应用内置资源安装软链至 `~/.local/bin` 或 `/usr/local/bin`），随后通过现有的 `daemon-manager.ts` 自动拉起后台 daemon。

**技术栈：** TypeScript, Node.js (`fs/promises`, `child_process`, `os`, `path`), Electron, Vitest

---

### 任务 1：创建 Self-Host 配置自动同步模块 (`cli-config-sync.ts`)

**文件：**
- 创建：`apps/desktop/src/main/cli-config-sync.ts`
- 测试：`apps/desktop/src/main/cli-config-sync.test.ts`

- [ ] **步骤 1：编写失败的单元测试**

```typescript
// apps/desktop/src/main/cli-config-sync.test.ts
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtemp, readFile, rm, writeFile, mkdir } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { syncCliSelfHostConfigPath } from "./cli-config-sync";

describe("syncCliSelfHostConfigPath", () => {
  let testDir: string;
  let configPath: string;

  beforeEach(async () => {
    testDir = await mkdtemp(join(tmpdir(), "multica-config-sync-test-"));
    configPath = join(testDir, "config.json");
  });

  afterEach(async () => {
    await rm(testDir, { recursive: true, force: true });
  });

  it("creates config.json with server_url and app_url when file does not exist", async () => {
    const changed = await syncCliSelfHostConfigPath(configPath, {
      apiUrl: "http://10.101.102.116:3095",
      appUrl: "http://10.101.102.116:3080",
    });

    expect(changed).toBe(true);
    const content = JSON.parse(await readFile(configPath, "utf-8"));
    expect(content.server_url).toBe("http://10.101.102.116:3095");
    expect(content.app_url).toBe("http://10.101.102.116:3080");
  });

  it("preserves existing configuration fields while updating server_url and app_url", async () => {
    await writeFile(
      configPath,
      JSON.stringify({
        device_name: "custom-device",
        max_concurrent_tasks: 4,
        server_url: "http://old-server:8080",
      }, null, 2),
      "utf-8"
    );

    const changed = await syncCliSelfHostConfigPath(configPath, {
      apiUrl: "http://10.101.102.116:3095",
      appUrl: "http://10.101.102.116:3080",
    });

    expect(changed).toBe(true);
    const content = JSON.parse(await readFile(configPath, "utf-8"));
    expect(content.device_name).toBe("custom-device");
    expect(content.max_concurrent_tasks).toBe(4);
    expect(content.server_url).toBe("http://10.101.102.116:3095");
    expect(content.app_url).toBe("http://10.101.102.116:3080");
  });

  it("returns false and avoids write when server_url and app_url are already up-to-date", async () => {
    await writeFile(
      configPath,
      JSON.stringify({
        server_url: "http://10.101.102.116:3095",
        app_url: "http://10.101.102.116:3080",
      }, null, 2),
      "utf-8"
    );

    const changed = await syncCliSelfHostConfigPath(configPath, {
      apiUrl: "http://10.101.102.116:3095",
      appUrl: "http://10.101.102.116:3080",
    });

    expect(changed).toBe(false);
  });
});
```

- [ ] **步骤 2：运行测试并验证其失败**

运行：`pnpm --filter desktop test src/main/cli-config-sync.test.ts`
预期：失败，显示模块 `cli-config-sync` 未定义。

- [ ] **步骤 3：编写最小化实现**

```typescript
// apps/desktop/src/main/cli-config-sync.ts
import { readFile, writeFile, mkdir } from "fs/promises";
import { dirname, join } from "path";
import { homedir } from "os";

export interface SelfHostUrls {
  apiUrl: string;
  appUrl: string;
}

export function defaultCliConfigPath(): string {
  return join(homedir(), ".multica", "config.json");
}

export async function syncCliSelfHostConfigPath(
  configPath: string,
  urls: SelfHostUrls
): Promise<boolean> {
  let existing: Record<string, unknown> = {};
  try {
    const raw = await readFile(configPath, "utf-8");
    existing = JSON.parse(raw);
  } catch {
    existing = {};
  }

  const currentServerUrl = existing["server_url"];
  const currentAppUrl = existing["app_url"];

  if (currentServerUrl === urls.apiUrl && currentAppUrl === urls.appUrl) {
    return false;
  }

  const updated = {
    ...existing,
    server_url: urls.apiUrl,
    app_url: urls.appUrl,
  };

  await mkdir(dirname(configPath), { recursive: true });
  await writeFile(configPath, JSON.stringify(updated, null, 2) + "\n", "utf-8");
  return true;
}

export async function syncCliSelfHostConfig(urls: SelfHostUrls): Promise<boolean> {
  return syncCliSelfHostConfigPath(defaultCliConfigPath(), urls);
}
```

- [ ] **步骤 4：运行测试并验证其通过**

运行：`pnpm --filter desktop test src/main/cli-config-sync.test.ts`
预期：测试全部通过。

- [ ] **步骤 5：提交代码**

```bash
git add apps/desktop/src/main/cli-config-sync.ts apps/desktop/src/main/cli-config-sync.test.ts
git commit -m "feat(desktop): add CLI self-host config sync module"
```

---

### 任务 2：创建 CLI 全局 PATH 探测与安装模块 (`cli-path-installer.ts`)

**文件：**
- 创建：`apps/desktop/src/main/cli-path-installer.ts`
- 测试：`apps/desktop/src/main/cli-path-installer.test.ts`

- [ ] **步骤 1：编写失败的单元测试**

```typescript
// apps/desktop/src/main/cli-path-installer.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { mkdtemp, rm, writeFile, chmod } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { ensureCliInPathWithDeps } from "./cli-path-installer";

describe("ensureCliInPathWithDeps", () => {
  let testDir: string;
  let sourceBin: string;
  let targetDir: string;

  beforeEach(async () => {
    testDir = await mkdtemp(join(tmpdir(), "multica-cli-path-test-"));
    sourceBin = join(testDir, "source-multica");
    targetDir = join(testDir, "bin");
    await writeFile(sourceBin, "#!/bin/sh\necho multica 1.0.0", "utf-8");
    await chmod(sourceBin, 0o755);
  });

  afterEach(async () => {
    await rm(testDir, { recursive: true, force: true });
  });

  it("does not install when CLI is already available on PATH", async () => {
    const isAvailableOnPath = vi.fn().mockResolvedValue(true);
    const result = await ensureCliInPathWithDeps({
      bundledBinaryPath: sourceBin,
      targetDirs: [targetDir],
      isAvailableOnPath,
    });

    expect(result.installed).toBe(false);
    expect(result.reason).toBe("already_on_path");
    expect(isAvailableOnPath).toHaveBeenCalled();
  });

  it("installs symlink to target directory when not on PATH", async () => {
    const isAvailableOnPath = vi.fn().mockResolvedValue(false);
    const result = await ensureCliInPathWithDeps({
      bundledBinaryPath: sourceBin,
      targetDirs: [targetDir],
      isAvailableOnPath,
    });

    expect(result.installed).toBe(true);
    expect(result.targetPath).toBe(join(targetDir, process.platform === "win32" ? "multica.exe" : "multica"));
  });
});
```

- [ ] **步骤 2：运行测试并验证其失败**

运行：`pnpm --filter desktop test src/main/cli-path-installer.test.ts`
预期：失败，显示模块 `cli-path-installer` 未定义。

- [ ] **步骤 3：编写最小化实现**

```typescript
// apps/desktop/src/main/cli-path-installer.ts
import { execFile } from "child_process";
import { mkdir, symlink, rm, copyFile, chmod } from "fs/promises";
import { existsSync } from "fs";
import { dirname, join } from "path";
import { homedir } from "os";

export interface PathInstallResult {
  installed: boolean;
  targetPath?: string;
  reason?: "already_on_path" | "installed" | "no_bundled_binary" | "all_targets_failed";
  error?: string;
}

export function defaultBinaryName(): string {
  return process.platform === "win32" ? "multica.exe" : "multica";
}

export function defaultCandidateDirs(): string[] {
  const dirs: string[] = [];
  if (process.platform !== "win32") {
    dirs.push(join(homedir(), ".local", "bin"));
    dirs.push("/usr/local/bin");
  } else {
    dirs.push(join(process.env.LOCALAPPDATA || join(homedir(), "AppData", "Local"), "Microsoft", "WindowsApps"));
  }
  return dirs;
}

export function checkCliOnPath(): Promise<boolean> {
  return new Promise((resolve) => {
    const cmd = process.platform === "win32" ? "where.exe" : "which";
    execFile(cmd, ["multica"], (err) => {
      resolve(!err);
    });
  });
}

export async function ensureCliInPathWithDeps(deps: {
  bundledBinaryPath: string;
  targetDirs?: string[];
  isAvailableOnPath?: () => Promise<boolean>;
}): Promise<PathInstallResult> {
  const isAvailable = deps.isAvailableOnPath ?? checkCliOnPath;
  if (await isAvailable()) {
    return { installed: false, reason: "already_on_path" };
  }

  if (!existsSync(deps.bundledBinaryPath)) {
    return { installed: false, reason: "no_bundled_binary" };
  }

  const targets = deps.targetDirs ?? defaultCandidateDirs();
  const binName = defaultBinaryName();

  for (const dir of targets) {
    const dest = join(dir, binName);
    try {
      await mkdir(dir, { recursive: true });
      await rm(dest, { force: true }).catch(() => {});
      if (process.platform === "win32") {
        await copyFile(deps.bundledBinaryPath, dest);
      } else {
        await symlink(deps.bundledBinaryPath, dest);
        await chmod(dest, 0o755).catch(() => {});
      }
      return { installed: true, targetPath: dest, reason: "installed" };
    } catch {
      // Try next directory if permission denied (e.g. /usr/local/bin without sudo)
      continue;
    }
  }

  return { installed: false, reason: "all_targets_failed" };
}

export async function ensureCliInPath(bundledBinaryPath: string): Promise<PathInstallResult> {
  return ensureCliInPathWithDeps({ bundledBinaryPath });
}
```

- [ ] **步骤 4：运行测试并验证其通过**

运行：`pnpm --filter desktop test src/main/cli-path-installer.test.ts`
预期：测试全部通过。

- [ ] **步骤 5：提交代码**

```bash
git add apps/desktop/src/main/cli-path-installer.ts apps/desktop/src/main/cli-path-installer.test.ts
git commit -m "feat(desktop): add CLI PATH detector and installer"
```

---

### 任务 3：集成至客户端启动生命周期与后台 Daemon 自动启动

**文件：**
- 修改：`apps/desktop/src/main/daemon-manager.ts`
- 修改：`apps/desktop/src/main/index.ts`

- [ ] **步骤 1：在 `daemon-manager.ts` 的 `bootstrapCli` 中接入配置同步与 PATH 安装**

在 `bootstrapCli` 执行前：
1. 调用 `syncCliSelfHostConfig({ apiUrl, appUrl })` 同步 self-host 服务端地址。
2. 调用 `ensureCliInPath(bundledCliPath())` 确保终端 PATH 中可用。
3. 随后继续执行原有解析，并在解析成功后确保 daemon 启动。

- [ ] **步骤 2：在 `main/index.ts` 中传递 runtimeConfig 并在初始化就绪时触发同步**

确保当 `runtimeConfig` 读取完毕后（无论是从 dev 环境变量还是打包后的配置），自动传递 `apiUrl` 和 `appUrl` 至 `bootstrapCli`。

- [ ] **步骤 3：运行客户端主进程相关测试**

运行：`pnpm --filter desktop test src/main/`
预期：所有现有与新增测试均无 regression 顺利通过。

- [ ] **步骤 4：提交代码**

```bash
git add apps/desktop/src/main/daemon-manager.ts apps/desktop/src/main/index.ts
git commit -m "feat(desktop): integrate CLI auto-install and self-host daemon startup"
```

---

### 任务 4：全局联调与自测验证

- [ ] **步骤 1：本地构建并运行客户端测试验证**
  - 执行 `pnpm --filter desktop build` 或在 dev 模式下启动。
  - 检查终端 `which multica` 与 `multica config` 输出是否已包含 `server_url: http://10.101.102.116:3095`。
  - 检查 `multica daemon status` 是否为 `running`。

- [ ] **步骤 2：最终提交与状态整理**
  - 验证所有 git 状态整洁无残留。
