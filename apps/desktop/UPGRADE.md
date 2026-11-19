# Multica 原生桌面端升级合并指南

本文档记录如何将上游 `multica-ai/multica` 新版本（例如 `v0.11.0`、`v0.12.0`）合入到本项目的 Wails 原生桌面端分支。

## 核心宗旨

1. **去掉外部 `multica` CLI 依赖**：客户端启动、探测、运行任务都不再依赖外部安装或调用 `multica` 二进制。
2. **CLI 与 Daemon 完全融入客户端**：
   - 原 `server/cmd/multica/` 下的命令行实现统一收拢在 `server/pkg/multicacli/`，`server/cmd/multica/main.go` 仅保留薄入口。
   - `daemon` 通过 `server/pkg/embeddeddaemon/` 以纯内存模式（`RunEmbedded` + `HealthSnapshot`，无本地 `/health` HTTP 端口）内嵌在桌面端 Go 进程中运行。
   - 智能体执行任务时，由客户端预加载任务上下文与 `./attachments/` 附件，不在提示词中要求智能体调用 `multica` CLI。

---

## 升级合并步骤

### 1. 确保当前分支工作区干净

合并前务必先把当前分支的所有本地修改提交，避免冲突时丢失现场：

```bash
# 查看当前分支与未提交修改
git branch --show-current
git status

# 如有未提交修改，按模块提交或先存一个 checkpoint：
git add -A && git commit -m "chore(desktop): checkpoint before upstream upgrade"
```

### 2. 配置远程并拉取上游代码与标签

先确认远程仓库配置（当前默认 `origin` 指向 `multica-ai/multica`；如果后续切到自己的 fork，可额外添加 `upstream`）：

```bash
git remote -v
# 若 origin 是个人 fork、尚未添加官方上游，可执行：
# git remote add upstream git@github.com:multica-ai/multica.git

# 拉取最新分支与全部 Release Tag
git fetch origin --tags --prune
# 如果配置了 upstream，则改为：git fetch upstream --tags --prune

# 查看最新的版本标签
git tag --sort=-v:refname | head -n 10
```

### 3. 基于开发主分支切出临时合并分支

不要直接在 `feat/wails-desktop-migration` 上硬合，先切出临时分支（例如 `merge/v0.12.0`），测试与打包都通过后再合回主开发分支：

```bash
git checkout feat/wails-desktop-migration
git checkout -b merge/<target-version>
```

### 4. 执行合并并处理冲突

```bash
git merge <target-version>
```

> 例如合并 `v0.11.0`：`git merge v0.11.0`。

如果出现冲突，按以下步骤定位和处理：

```bash
# 1) 列出所有冲突文件
git status --short
git diff --name-only --diff-filter=U

# 2) 查看上游在本次版本对关键目录的改动清单（防止漏掉新增文件或接口）
git diff --stat feat/wails-desktop-migration..<target-version> -- \
  server/cmd/multica/ \
  server/internal/daemon/ \
  apps/desktop/src/preload/ \
  apps/desktop/src/renderer/src/
```

冲突处理原则：
- **纯删除/重命名类冲突**（例如上游改了旧 Electron 主进程文件，而本地 Wails 分支已删除它）：确认本地无需保留该文件后执行 `git rm <file>`。
- **核心业务与公共包冲突**（`packages/`、`server/`、`apps/desktop/src/renderer/`）：**不要整文件 `--ours` 或 `--theirs`**，必须逐块人工合并，既吸纳上游新功能，又保留下面的“重点冲突与同步检查清单”中的桌面端改造。
- 解决完某个文件后标记为已解决：
  ```bash
  git add <file>
  ```
- 全部冲突解决并完成下方检查清单后，提交合并结果：
  ```bash
  git commit -m "Merge tag '<target-version>' into merge/<target-version>"
  ```
  （若中途需要放弃本次合并重试，可执行 `git merge --abort`。）

### 5. 验证无误后合回开发主分支

在临时合并分支完成测试与打包验证后，再切回 `feat/wails-desktop-migration` 合并并清理临时分支：

```bash
git checkout feat/wails-desktop-migration
git merge --ff-only merge/<target-version>
git branch -d merge/<target-version>
```

---

## 重点冲突与同步检查清单

### 1. `server/cmd/multica/` → `server/pkg/multicacli/`

- Git 的重命名检测会自动把上游对既有 CLI 文件的修改合并进 `server/pkg/multicacli/`。
- **检查上游是否新增了文件**（执行 `ls server/cmd/multica/`）：
  - 除了 `main.go` 之外的新增 `.go` / `_test.go` 文件，全部移动到 `server/pkg/multicacli/`：
    ```bash
    mv server/cmd/multica/<new_file>.go server/pkg/multicacli/
    sed -i '' '1s/^package main$/package multicacli/' server/pkg/multicacli/<new_file>.go
    ```
  - 如果上游在 `server/cmd/multica/main.go` 注册了新命令（如 `rootCmd.AddCommand(xxxCmd)`），同步添加到 `server/pkg/multicacli/root.go`，并将 `server/cmd/multica/main.go` 保持为仅调用 `multicacli.Execute(...)` 的薄入口。
  - 若新增测试中直接调用了 `main()`，改为 `os.Exit(Execute(...))`。

### 2. `server/internal/daemon/`（保持去 CLI 化与纯内嵌模式）

重点检查以下文件：
- `server/internal/daemon/daemon.go` & `health.go`：
  - 保留 `RunEmbedded(ctx)` 与 `HealthSnapshot(startedAt)`，确保内嵌模式不监听本地 HTTP 端口。
  - 保留内部 Mika 线程分发逻辑，并对齐上游 `d.runner.run` 参数变化（如 `slot.index`）。
  - 保留 `prompt, skillInvocation := buildRuntimePrompt(...)` 调用在 `WithPreloadedIssueContext(preloaded)` **之后**，确保预加载上下文生效的同时不丢失上游新增的 `skillInvocation` 等字段。
- `server/internal/daemon/prompt.go`：
  - 保留 `formatPreloadedIssueBlock(opts.preloadedIssue)` 拼接以及“禁止调用 `multica` CLI、直接输出最终回复”的指令；同时保留上游新增的上下文块（如 `explicitSkillSelectionBlock`）。
- `server/internal/daemon/execenv/runtime_config_sections.go`：
  - 检查上游是否新加了引导调用 `multica ...` CLI 的提示词，按需保持一致。

### 3. `apps/desktop/`（Wails 壳子与前端桥接）

- **`apps/desktop/src/renderer/src/main.tsx`**：
  - 保持在 `async function bootstrap()` 中先执行 `await initWailsDesktopAPI()`，再执行上游新增的初始化（如 `registerEE()`）和 `ReactDOM.createRoot(...)`。
- **`apps/desktop/src/preload/index.d.ts` ↔ `apps/desktop/src/renderer/src/platform/wails-desktop-api.ts`**：
  - 对比 `git diff <base>..<target-version> -- apps/desktop/src/preload/index.d.ts`，如果上游给 `DesktopAPI`、`DaemonAPI`、`UpdaterAPI` 或 `ElectronAPI` 新增了方法（例如 `window.updater.getDownloadState` / `onDownloadState`），**必须**在 `wails-desktop-api.ts` 中补齐对应实现或安全空实现，否则前端调用时会抛错导致 `WorkspaceRouteLayout` 被卸载、页面卡在“正在加载工作区...”。
- **版本号同步**：
  - 合并后检查 `apps/desktop/package.json` 的 `version`，同步更新：
    - `apps/desktop/app.go` 中的 `GetAppInfo().Version`
    - `apps/desktop/daemon_service.go` 中的 `CLIVersion`
    - `server/pkg/embeddeddaemon/manager.go` 中的默认 `CLIVersion`
  - **保持 `LaunchedBy: "desktop"`**（不要改成其他字符串，否则前端 `UpdateSection` 会误判为独立 CLI 并弹出无效更新按钮）。
  - 打包脚本 `apps/desktop/scripts/build-macos.sh` 与 `build-windows.sh` 已改为自动读取 `apps/desktop/package.json` 的 `version`，无需手动修改。

### 4. `packages/views/` 与 `packages/ui/`

确认以下桌面端兼容修复在合并后依然保留：
- `packages/ui/components/common/file-upload-button.tsx`、`packages/views/chat/components/chat-add-menu.tsx`、`packages/views/common/avatar-upload-control.tsx`：优先使用 `window.desktopAPI.pickFiles` 调起原生文件选择。
- `packages/views/projects/components/project-resources-section.tsx`：`ExecutionModeChip` 在本机切换模式时重新调用 `validateLocalDirectory`。
- `packages/views/modals/issue-picker-modal.tsx`：无 `suggestions` 时默认通过 `api.listIssues` 加载工作区任务列表。
- `packages/views/skills/components/runtime-local-skill-import-panel.tsx`：默认优先选中 `status === "online"` 的本地运行时。

---

## 合并后验证与打包

### 1. 更新依赖（若上游新增了 workspace 包或依赖）

```bash
pnpm install
```

### 2. 运行测试

```bash
# 服务端嵌入模块 & CLI 测试
cd server && go test ./pkg/embeddeddaemon/... ./pkg/multicacli/... ./cmd/multica/...

# 桌面端 Go 测试（含 PATH 修复、ProxyFetch 等）
cd ../apps/desktop && go test -v .

# 桌面端前端单测
cd ../apps/desktop && pnpm test
```

### 3. 打包 macOS 与 Windows 客户端

```bash
cd apps/desktop && pnpm package:all-native
```

打包完成后，检查 `apps/desktop/dist/` 下生成对应新版本号的产物：
- `Multica.app`
- `Multica-<version>-arm64.dmg`
- `Multica-<version>-arm64.zip`
- `Multica-<version>-win-x64/Multica.exe`
- `Multica-<version>-win-x64.zip`
