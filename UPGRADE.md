# Code-OPC 桌面端版本升级与合并指南

本文档专门为 `code-opc` 仓库定制，详细说明如何基于官方 `multica-ai/multica` 的新版本 Release Tag（如 `v0.11.1`、`v0.12.0` 等）进行版本升级合并，并通过 `code-opc` 自己的 Git 源（`shtokendance/code-opc.git`）完成全生命周期管理。

---

## 一、核心宗旨与技术边界

在升级过程中，必须始终保持当前项目的核心改造宗旨，不能被官方上游代码冲掉：

1. **去掉外部 `multica` CLI 依赖**：客户端运行任务、健康探测、代码执行，完全不依赖系统全局安装或调用外部 `multica` 二进制程序。
2. **CLI 实现收拢**：官方原 `server/cmd/multica/` 目录下的 CLI 实现全部收归在 `server/pkg/multicacli/`，`server/cmd/multica/main.go` 仅保留调用 `multicacli.Execute(...)` 的薄入口。
3. **Daemon 纯内存嵌入模式**：通过 `server/pkg/embeddeddaemon/` 运行，使用 `RunEmbedded` 与 `HealthSnapshot`，**不监听本地 HTTP `/health` 端口**，消除端口冲突与多开问题。
4. **Wails 原生架构**：桌面端彻底使用 Wails（Go 原生壳 + WebView）替代 Electron，所有前端桥接均在 `apps/desktop/src/renderer/src/platform/wails-desktop-api.ts` 中维护。

---

## 二、Git 双 Remote 架构配置

本仓库采用标准的企业双远程源架构：

| Remote 名称 | 远程地址 | 用途 |
|---|---|---|
| **`origin`** | `git@github.com:shtokendance/code-opc.git` | **企业私有主源**：日常开发、特性分支、升级分支推送、PR 评审、发布 Tag |
| **`upstream`** | `git@github.com:multica-ai/multica.git` | **官方开源上游**：只读源，仅用于 `fetch` 官方最新代码与 Release 标签 |

### 验证远程源配置
在项目根目录执行：
```bash
git remote -v
```
预期输出：
```text
origin    git@github.com:shtokendance/code-opc.git (fetch)
origin    git@github.com:shtokendance/code-opc.git (push)
upstream  git@github.com:multica-ai/multica.git (fetch)
upstream  git@github.com:multica-ai/multica.git (push)
```
*(若缺少 upstream，可执行 `git remote add upstream git@github.com:multica-ai/multica.git`)*

---

## 三、标准版本升级步骤（以升级 v0.11.1 为例）

### 步骤 1：确保工作区干净并切换基线分支

合并前先确保本地没有未提交的改动：
```bash
git status

# 切换到当前桌面的主力开发分支并拉取 origin 最新进展
git checkout feature/desktop-embedded-daemon
git pull origin feature/desktop-embedded-daemon
```

### 步骤 2：从官方 upstream 拉取最新 Release Tags

```bash
git fetch upstream --tags --prune

# 查看官方最新的版本标签列表
git tag --sort=-v:refname | head -n 10
```

### 步骤 3：切出专属升级分支

> **重要原则**：永远不要在主分支上直接 `merge`，切出独立的升级分支进行合并、排查与测试。

```bash
# 基于当前分支切出以版本号命名的临时升级分支
git checkout -b upgrade/v0.11.1
```

### 步骤 4：执行合并

```bash
git merge v0.11.1
```

---

## 四、冲突排查与关键目录审查（重点）

执行合并后，使用以下命令检查官方在本次版本改动了哪些敏感模块：

```bash
# 1. 查看上游变动概览
git diff --stat HEAD..v0.11.1 -- \
  server/cmd/multica/ \
  server/internal/daemon/ \
  apps/desktop/src/preload/ \
  apps/desktop/src/renderer/src/

# 2. 如果产生冲突，列出未解决冲突的文件
git status --short
git diff --name-only --diff-filter=U
```

### 必须审查的 4 大检查点：

#### 检查点 1：CLI 代码归拢 (`server/cmd/multica/` → `server/pkg/multicacli/`)
- 检查 `server/cmd/multica/` 下是否有官方新增加的 `.go` 文件：
  - 如果有，将其移至 `server/pkg/multicacli/`：
    ```bash
    mv server/cmd/multica/<new_file>.go server/pkg/multicacli/
    # 将包名从 package main 改为 package multicacli
    sed -i '' '1s/^package main$/package multicacli/' server/pkg/multicacli/<new_file>.go
    ```
  - 若 `server/cmd/multica/main.go` 中注册了新子命令（如 `rootCmd.AddCommand(...)`），同步转移到 `server/pkg/multicacli/root.go` 中，确保 `server/cmd/multica/main.go` 保持纯粹的薄入口。

#### 检查点 2：Daemon 纯内嵌模式与去 CLI 提示词
- **`server/internal/daemon/daemon.go` & `health.go`**：
  - 必须保留 `RunEmbedded(ctx)` 与 `HealthSnapshot`，绝对不要让 daemon 开启本地 HTTP 端口。
  - 保留任务分发时的 `WithPreloadedIssueContext`，由 Go 进程提前把任务上下文和附件拉取到任务目录。
- **`server/internal/daemon/prompt.go`**：
  - 必须保留提示词中的 `formatPreloadedIssueBlock`，并维持明确禁止智能体调用 `multica` CLI 的指令。

#### 检查点 3：Preload API 对齐（防前端卡死白屏）
- 对比检查：`git diff HEAD..v0.11.1 -- apps/desktop/src/preload/index.d.ts`
- **原因**：官方 Electron 端会在 `preload/index.d.ts` 声明 IPC 方法（例如 `window.updater.getDownloadState`）。
- **处理方式**：如果官方新增了任何方法，**必须**同步在 `apps/desktop/src/renderer/src/platform/wails-desktop-api.ts` 中补齐对应实现或安全的空兜底。如果缺失，前端页面加载时报未定义错误会导致整个工作区视图卸载，页面无限卡在“正在加载工作区...”。

#### 检查点 4：版本号与更新标识同步
- 打开 `apps/desktop/package.json`，将 `version` 更改为目标版本（如 `0.11.1`）。
- 同步检查以下文件的版本字段：
  - `apps/desktop/app.go` 中的 `GetAppInfo().Version`
  - `apps/desktop/daemon_service.go` 中的 `CLIVersion`
  - `server/pkg/embeddeddaemon/manager.go` 中的默认 `CLIVersion`
- **千万保持** `LaunchedBy: "desktop"`，切勿改动，否则前端会误判为独立 CLI 并弹出多余的无效更新卡片。

---

## 五、验证与本地打包

完成冲突解决和代码审查后，在合并分支上执行完整测试：

```bash
# 1. 编译与运行嵌入式守护进程及 CLI 单测
cd server
go test ./pkg/embeddeddaemon/... ./pkg/multicacli/...

# 2. 运行桌面端测试（PATH 修复、通知桥接、本地 API 等）
cd ../apps/desktop
go test -v .

# 3. 前端与全平台客户端打包验证
pnpm package:all-native
```

打包成功后，`apps/desktop/dist/` 会输出带有最新版本号的安装包：
- `Multica.app`
- `Multica-<version>-arm64.dmg`
- `Multica-<version>-arm64.zip`
- `Multica-<version>-win-x64.zip`

---

## 六、推送到企业 Git 源并完成发布

所有验证通过后，将成果通过企业 Git 源（`origin`）完成合流与发版：

```bash
# 1. 提交所有合并成果
git add -A
git commit -m "chore(upgrade): merge upstream v0.11.1 into desktop embedded daemon"

# 2. 将升级分支推送到你的 code-opc Git 源（origin）
git push -u origin upgrade/v0.11.1

# 3. 在 GitHub (shtokendance/code-opc) 提交 Pull Request，合入 feature/desktop-embedded-daemon
# 并在本地合回：
git checkout feature/desktop-embedded-daemon
git merge --ff-only upgrade/v0.11.1
git branch -d upgrade/v0.11.1

# 4. 在企业源上打上发布 Tag 并推送
git tag -a v0.11.1 -m "Release v0.11.1 with embedded daemon for code-opc"
git push origin v0.11.1
```

---

## 七、常见问题速查 (FAQ)

- **Q: 为什么页面卡在“正在加载工作区...”？**  
  **A**: 99% 的原因是官方在 `preload/index.d.ts` 新增了 IPC 方法，而 `wails-desktop-api.ts` 没提供。打开浏览器/WebView 控制台查看报错，在 `wails-desktop-api.ts` 补齐该方法即可。

- **Q: 为什么智能体列表显示“离线”？**  
  **A**: 守护进程已自动连接工作区并上线本机的 4 个运行时（`antigravity`、`claude`、`codex`、`pi`）。如果历史智能体绑定的是旧的 `runtime_id` 或旧电脑设备名，点击智能体属性面板手动将“运行时”切换为当前在线的本机运行时即可。

- **Q: 升级后外部 CLI 提示词死灰复燃？**  
  **A**: 检查 `server/internal/daemon/prompt.go`，确保 `formatPreloadedIssueBlock` 没有被官方合并覆盖，提示词末尾必须保留“禁止调用 multica CLI”的强约束。
