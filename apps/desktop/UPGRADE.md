# Multica 原生桌面端升级合并指南

> 详细的企业级升级与 Git 源运维指南已统一整理在仓库根目录：[UPGRADE.md](../../UPGRADE.md)。

---

## 升级核心速记

1. **版本拉取**：`git fetch upstream --tags --prune`
2. **切分支**：`git checkout -b upgrade/<version> feature/desktop-embedded-daemon`
3. **合并**：`git merge <version>`
4. **4 大关键检查**：
   - `server/cmd/multica/` 新文件移入 `server/pkg/multicacli/`，保留 `main.go` 薄入口。
   - `server/internal/daemon/` 保持无 HTTP 端口的纯内嵌模式（`RunEmbedded`），并保留去 CLI 提示词。
   - `apps/desktop/src/preload/index.d.ts` 变更必须同步到 `wails-desktop-api.ts`（防止前端卡死）。
   - `apps/desktop/package.json` 同步最新版本号，保持 `LaunchedBy: "desktop"`。
5. **测试与打包**：
   - `cd server && go test ./pkg/embeddeddaemon/... ./pkg/multicacli/...`
   - `cd apps/desktop && go test -v . && pnpm package:all-native`
6. **推送到企业源**：`git push -u origin upgrade/<version>`
