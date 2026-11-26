# CodeOPC Desktop 打包与发布说明

本文档说明 `apps/desktop` 桌面端（macOS / Windows）的测试版与正式版统一打包、Apple 签名与公证，以及服务端更新分发流程。

---

## 1. 环境配置

打包脚本会根据 `--env` 参数自动切换端点配置：

| 环境 | 配置文件 | `apiUrl` | `appUrl` | `platformUrl` |
| --- | --- | --- | --- | --- |
| 测试环境 (`test`) | `apps/desktop/config/endpoints.test.json` | `http://10.101.102.116:3088` | `http://10.101.102.116:3080` | `https://shtokendance.benlai.com` |
| 正式环境 (`prod`) | `apps/desktop/config/endpoints.prod.json` | `https://code-opc.benlai.com` | `https://code-opc.benlai.com` | `https://aiwork.benlai.com` |

> 打包时，`build-macos.sh` 与 `build-windows.sh` 会读取对应 JSON，将端点注入前端构建环境变量，并通过 Go `//go:embed` / `-ldflags` 内嵌到客户端二进制中。

---

## 2. 签名与 Apple 公证准备（仅初次配置）

### 2.1 Developer ID 证书
- `release.sh` 会自动检查本机 Keychain 中是否存在 `Developer ID Application: ...` 证书。
- 若本机未导入，脚本会尝试从仓库根目录的 `apple-signing/developer-id-full.p12`（或 `apple-signing.zip`）导入（正式版强制要求签名）。

### 2.2 Apple 公证凭据（Notarization）
正式版对外分发必须经过 Apple 公证，否则 macOS Gatekeeper 会拦截提示「无法验证开发者」。推荐在打包机执行一次以下命令将凭据存入本机 Keychain：

```bash
xcrun notarytool store-credentials "codeopc-notary" \
  --apple-id "<your-apple-id>" \
  --team-id "Q35YK5Q9F3" \
  --password "<your-app-specific-password>"
```

- `password` 需使用在 [account.apple.com](https://account.apple.com)「登录与安全 → App 专用密码」中生成的专用密码，而非 Apple ID 登录密码。
- 也可在运行时改用环境变量 `APPLE_ID` + `APPLE_APP_SPECIFIC_PASSWORD`（可选 `APPLE_TEAM_ID`）。

---

## 3. 执行打包命令

在 `apps/desktop` 目录下执行 `scripts/release.sh`：

```bash
cd apps/desktop
```

### 3.1 仅打测试版（并上传到测试服）
```bash
bash scripts/release.sh --env test --upload
```

### 3.2 仅打正式版（含签名 + Apple 公证）
```bash
APPLE_NOTARY_PROFILE="codeopc-notary" REQUIRE_APPLE_NOTARIZATION=1 \
  bash scripts/release.sh --env prod
```

### 3.3 升级版本号并同时打测试版 + 正式版
```bash
APPLE_NOTARY_PROFILE="codeopc-notary" REQUIRE_APPLE_NOTARIZATION=1 \
  bash scripts/release.sh --env both --version 0.11.25 --upload
```

### 常用参数
- `--env <test|prod|both>`：必填，指定打包目标环境。
- `--version <X.Y.Z>`：可选，更新 `apps/desktop/package.json` 版本号后再打包。
- `--upload`：可选，上传测试环境产物到测试服务器（正式版忽略该参数，需人工确认后发布）。
- `REQUIRE_APPLE_NOTARIZATION=1`：可选，强制要求完成 Apple 公证，未配置公证凭据时直接报错中止。

---

## 4. 打包内部流程

1. **构建前端与端侧二进制** (`build-macos.sh` / `build-windows.sh`)
   - 编译 Electron/Wails 前端资源与 Go 主程序、内置 CLI。
2. **macOS `.app` 签名**
   - 使用 `Developer ID Application` 证书执行 `codesign --force --deep --options runtime --timestamp`，开启 Hardened Runtime 与时间戳。
3. **macOS `.app` 公证与装订**（配置了公证凭据时执行）
   - 压缩 `.app` 提交 `xcrun notarytool submit --wait`。
   - 公证通过后执行 `xcrun stapler staple`、`xcrun stapler validate` 与 `spctl -a -vvv -t exec` 校验。
4. **生成分发包**
   - 打包 macOS `.zip`（用于自动更新，内含已装订公证票据的 `.app`）。
   - 生成 macOS `.dmg` 并执行 `codesign` 签名；若启用了公证，同时对 `.dmg` 执行 `notarytool submit` 与 `stapler staple`。
   - 交叉编译 Windows `.exe` 并生成 `.zip`。
5. **生成更新清单** (`scripts/generate-manifest.mjs`)
   - 计算各平台 `.zip` 的 SHA256 与文件大小，生成 `latest.json` 与 `latest-codeopc.json`。

---

## 5. 产物目录

| 环境 | 输出目录 | 主要产物 |
| --- | --- | --- |
| 测试版 (`test`) | `apps/desktop/dist/test/` | `CodeOPC-<ver>-arm64.dmg`、`CodeOPC-<ver>-arm64.zip`、`CodeOPC-<ver>-win-x64.zip`、`latest.json`、`latest-codeopc.json` |
| 正式版 (`prod`) | `apps/desktop/dist/release/` | `CodeOPC-<ver>-arm64.dmg`、`CodeOPC-<ver>-arm64.zip`、`CodeOPC-<ver>-win-x64.zip`、`latest.json`、`latest-codeopc.json` |

---

## 6. 服务端更新接口与存放目录

正式服与测试服的后端均由 `server/internal/handler/desktop_updates.go` 提供更新检查与下载服务：

- **检查更新接口**：
  `GET /api/desktop/updates/check?platform=<darwin|windows>&arch=<arm64|amd64>&version=<当前版本>&product=CodeOPC`
- **下载更新包接口**：
  `GET /api/desktop/updates/download/{filename}`
  - 正式环境示例：`https://code-opc.benlai.com/api/desktop/updates/download/CodeOPC-0.11.24-arm64.dmg`
- **服务器文件目录**：
  优先读取环境变量 `MULTICA_RELEASES_DIR`；未设置时依次查找 `/data/releases`、`/app/releases`、`/app/data/releases`、`./releases`。将对应环境的产物（`.zip`、`.dmg`、`latest.json`、`latest-codeopc.json`）放入该目录即可生效。

---

## 7. 发布前自检

```bash
# 1. 验证 .app Gatekeeper 评估通过
spctl -a -vvv -t exec apps/desktop/dist/CodeOPC.app
# 预期输出包含: accepted / source=Notarized Developer ID

# 2. 验证 .dmg 公证票据装订
xcrun stapler validate apps/desktop/dist/release/CodeOPC-<ver>-arm64.dmg
# 预期输出: The validate action worked!
```
