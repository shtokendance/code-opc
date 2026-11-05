# Desktop CLI Auto-Install and Self-Host Auto-Configure Design

## 1. Background & Goals
In TokenDance Code (`code-opc`), users and AI coding agents rely on the `multica` CLI and its background daemon process to execute and manage tasks. Currently:
- The desktop app only manages a private CLI copy in Electron `userData/bin/multica`, without ensuring it is accessible from the user's terminal `$PATH`.
- When connecting to a self-hosted server (e.g. `http://10.101.102.116:3095`), `~/.multica/config.json` is not automatically seeded with `server_url` and `app_url`, requiring manual `multica setup self-host`.
- When the app starts, if the CLI or daemon is not running with self-host configuration, tasks and runtimes appear offline.

**Goals**:
1. When `code-opc` desktop starts, automatically detect if `multica` CLI is installed and available in `$PATH`.
2. If not installed or accessible from `$PATH`, automatically install / symlink the bundled binary to a user-accessible `$PATH` directory (e.g. `~/.local/bin/multica` or `/usr/local/bin/multica`).
3. Automatically configure self-host server settings: read `runtimeConfig` (`apiUrl`, `appUrl`) and inject them into `~/.multica/config.json` if missing or drifted.
4. Automatically start the background daemon pointing to the self-hosted server, keeping the runtime online.

---

## 2. Technical Architecture

### 2.1 Component Flow
```
Electron Main Process (app.whenReady / bootstrapCli)
  │
  ├── 1. Read RuntimeConfig (apiUrl: e.g. http://10.101.102.116:3095, appUrl: http://10.101.102.116:3080)
  │
  ├── 2. Auto-configure Self-Host in ~/.multica/config.json
  │      - Read existing config or create new
  │      - Set server_url = runtimeConfig.apiUrl
  │      - Set app_url = runtimeConfig.appUrl
  │      - Atomic save to disk
  │
  ├── 3. CLI Detection & Global Path Installation
  │      - Find existing multica binary via PATH probe or known locations
  │      - If missing in PATH:
  │          - Resolve bundled binary (resources/bin/multica or app.asar.unpacked)
  │          - Target directory selection:
  │              1. ~/.local/bin (create if missing; add to user PATH if needed)
  │              2. /usr/local/bin (if writable without sudo)
  │          - Create symlink or executable copy
  │
  └── 4. Background Daemon Auto-Start
         - Check daemon health on configured port
         - If stopped, spawn `multica daemon start`
         - Sync daemon status with renderer via IPC
```

---

## 3. Detailed Design

### 3.1 Self-Host Configuration Sync (`cli-config-sync.ts`)
- Target path: `~/.multica/config.json`.
- When `runtimeConfig` is loaded, inspect `config.json`.
- If `server_url` or `app_url` does not match `runtimeConfig.apiUrl` / `runtimeConfig.appUrl`:
  - Preserve any existing keys (`device_name`, `max_concurrent_tasks`, `workspaces_root`, etc.).
  - Update `server_url` and `app_url`.
  - Save JSON formatted with 2 spaces.

### 3.2 CLI Path Auto-Link & Installation (`cli-path-installer.ts`)
- Check whether `multica` can be executed from `$PATH` using `execFile("multica", ["--version"])`.
- If not found in `$PATH`:
  - Locate bundled CLI binary:
    - In dev mode: `apps/desktop/resources/bin/multica`
    - In production: `process.resourcesPath/bin/multica` (or `app.asar.unpacked/resources/bin/multica`)
  - Candidates for symlink:
    - Primary candidate: `join(homedir(), ".local", "bin", "multica")`. Ensure `~/.local/bin` exists with `0o755`.
    - If `/usr/local/bin` is writable, also attempt symlink there.
  - On macOS, execute ad-hoc codesign `codesign -s - --force <path>` if needed.
  - In addition, ensure desktop's managed CLI `userData/bin/multica` is refreshed.

### 3.3 Daemon Auto-Start Integration
- In `apps/desktop/src/main/daemon-manager.ts`:
  - After `cli-config-sync` and CLI installation complete:
    - Ensure `desiredDaemonRunning` is set to true by default for desktop sessions.
    - Invoke `startDaemon()`.
    - `startDaemon()` invokes `multica daemon start`, which reads the newly written `~/.multica/config.json`.

---

## 4. Error Handling & Safety
1. **Permission Denied (EACCES)**: If `/usr/local/bin` is not writable, fallback cleanly to `~/.local/bin` or `userData/bin` without crashing or throwing unhandled errors.
2. **Offline / Unreachable Server**: If the self-host backend is temporarily down or waking up, write config anyway, attempt daemon start, and allow periodic health checks to report status.
3. **Idempotency**: All operations (config merge, symlink creation, daemon check) must be idempotent and non-blocking.
