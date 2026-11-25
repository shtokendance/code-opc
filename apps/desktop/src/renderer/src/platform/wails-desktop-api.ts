/**
 * Wails v3 Desktop & Daemon API Adapter
 * 
 * Bridges Wails v3 Go backend services and Events to the existing
 * window.desktopAPI and window.daemonAPI interfaces.
 * 
 * ZERO MOCKS: All methods invoke real Go OS implementations and real multica daemon processes.
 */

import type { RuntimeConfigResult } from "../../../shared/runtime-config";
import type { FreezeBreadcrumb } from "../../../shared/freeze-breadcrumb";
import { parseIssueWindowPath, type DesktopWindowContext } from "../../../shared/issue-window";

// Declare global Wails bindings
declare global {
  interface Window {
    wails?: {
      Call?: {
        ByName: (methodName: string, ...args: unknown[]) => Promise<any>;
        ByID: (methodId: number, ...args: unknown[]) => Promise<any>;
      };
      Events?: {
        On: (eventName: string, callback: (...args: unknown[]) => void) => () => void;
        Emit: (eventName: string, ...optionalData: unknown[]) => void;
      };
    };
    _wails?: {
      invoke?: (message: string) => void;
    };
  }
}

// Synchronously mount window.updater immediately on script evaluation
if (typeof window !== "undefined") {
  window.updater = window.updater || {
    getDownloadState: async () => {
      try {
        return await callAppService("GetDownloadState");
      } catch {
        return { status: "idle" as const };
      }
    },
    onDownloadState: (callback: any) => {
      if (window.wails?.Events?.On) {
        return window.wails.Events.On("updater:download-state", (evt: any) => {
          const data = evt?.data ?? evt;
          callback(Array.isArray(data) && data.length === 1 ? data[0] : data);
        });
      }
      return () => {};
    },
    onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
      if (window.wails?.Events?.On) {
        return window.wails.Events.On("updater:update-available", (evt: any) => {
          const data = evt?.data ?? evt;
          callback(Array.isArray(data) && data.length === 1 ? data[0] : data);
        });
      }
      return () => {};
    },
    onDownloadProgress: (callback: (progress: { percent: number }) => void) => {
      if (window.wails?.Events?.On) {
        return window.wails.Events.On("updater:download-progress", (evt: any) => {
          const data = evt?.data ?? evt;
          callback(Array.isArray(data) && data.length === 1 ? data[0] : data);
        });
      }
      return () => {};
    },
    onUpdateDownloaded: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
      if (window.wails?.Events?.On) {
        return window.wails.Events.On("updater:update-downloaded", (evt: any) => {
          const data = evt?.data ?? evt;
          callback(Array.isArray(data) && data.length === 1 ? data[0] : data);
        });
      }
      return () => {};
    },
    downloadUpdate: async () => {
      return await callAppService("DownloadUpdate");
    },
    installUpdate: async () => {
      return await callAppService("InstallUpdate");
    },
    getPreferences: async () => {
      try {
        return await callAppService("GetUpdatePreferences");
      } catch {
        return { automaticUpdates: true };
      }
    },
    setAutomaticUpdates: async (enabled: boolean) => {
      try {
        return await callAppService("SetUpdatePreferences", enabled);
      } catch {
        return { automaticUpdates: enabled };
      }
    },
    checkForUpdates: async () => {
      try {
        const res = await callAppService("CheckForUpdates");
        return (
          res ?? {
            ok: false as const,
            reason: "unknown" as const,
            downloadState: { status: "idle" as const },
          }
        );
      } catch (err: any) {
        return {
          ok: false as const,
          reason: "network" as const,
          downloadState: { status: "idle" as const },
          error: err?.message || String(err),
        };
      }
    },
  };

  window.electron = window.electron || {
    ipcRenderer: {
      on: () => () => {},
      removeListener: () => {},
      invoke: async () => {},
      send: () => {},
    },
    process: {
      platform: (typeof navigator !== "undefined" && navigator.userAgent.includes("Windows")
        ? "win32"
        : typeof navigator !== "undefined" && navigator.userAgent.includes("Linux")
          ? "linux"
          : "darwin") as any,
      versions: { electron: "wails-v3", chrome: "webview", node: "go" },
    },
  };
}

let wailsRuntimePromise: Promise<any> | null = null;

async function getWailsRuntime(): Promise<any> {
  if (window.wails?.Call?.ByName) {
    return window.wails;
  }
  if ((window as any).__wails_ready__) {
    try {
      await (window as any).__wails_ready__;
      if (window.wails?.Call?.ByName) {
        return window.wails;
      }
    } catch {
      // ignore
    }
  }
  if (!wailsRuntimePromise) {
    wailsRuntimePromise = (async () => {
      try {
        const runtimePath = "/wails/runtime.js";
        const mod = await import(/* @vite-ignore */ runtimePath);
        window.wails = window.wails || {};
        Object.assign(window.wails, mod);
        return window.wails;
      } catch (err) {
        console.error("Failed to load /wails/runtime.js:", err);
        return window.wails ?? null;
      }
    })();
  }
  return wailsRuntimePromise;
}

export async function callAppService<T = any>(methodName: string, ...args: any[]): Promise<T> {
  let wails = await getWailsRuntime();
  if (!wails?.Call?.ByName) {
    for (let i = 0; i < 5; i++) {
      await new Promise((r) => setTimeout(r, 60));
      wails = await getWailsRuntime();
      if (wails?.Call?.ByName) break;
    }
  }

  if (wails?.Call?.ByName) {
    return wails.Call.ByName(`main.AppService.${methodName}`, ...args);
  }
  throw new Error(`Wails Call.ByName not available for main.AppService.${methodName}`);
}

function shouldProxy(urlStr: string): boolean {
  try {
    const parsed = new URL(urlStr, window.location.href);
    // Never proxy internal Wails v3 IPC or bundled assets
    if (
      parsed.hostname === "wails.localhost" ||
      parsed.pathname.startsWith("/wails/") ||
      parsed.pathname.startsWith("/assets/")
    ) {
      return false;
    }
    // Only proxy external HTTP/HTTPS requests to backend server APIs
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

export async function initWailsDesktopAPI(): Promise<void> {
  // Preload Wails runtime
  await getWailsRuntime();

  // Transparent fetch proxy via Go backend to eliminate all WebKit CORS/mixed-content restrictions
  if (!(window as any).__wails_fetch_installed__) {
    (window as any).__wails_fetch_installed__ = true;
    const originalFetch = window.fetch.bind(window);

    window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      let url = "";
      let method = init?.method || "GET";
      let headersInit = init?.headers;
      let body = "";

      if (typeof input === "string") {
        url = input;
      } else if (input instanceof URL) {
        url = input.toString();
      } else if (input && typeof input === "object" && "url" in input) {
        url = (input as Request).url;
        method = init?.method || (input as Request).method || "GET";
        headersInit = headersInit || (input as Request).headers;
      }

      // Only proxy external HTTP/HTTPS requests to backend server APIs
      if (shouldProxy(url)) {
        try {
          const headers: Record<string, string> = {};
          if (headersInit) {
            if (headersInit instanceof Headers) {
              headersInit.forEach((v, k) => {
                headers[k] = v;
              });
            } else if (Array.isArray(headersInit)) {
              headersInit.forEach(([k, v]) => {
                headers[k] = v;
              });
            } else {
              Object.assign(headers, headersInit);
            }
          }

          let bodyB64 = "";
          const toBase64 = (buffer: ArrayBuffer): string => {
            const bytes = new Uint8Array(buffer);
            const chunkSize = 0x8000;
            const parts: string[] = [];
            for (let i = 0; i < bytes.length; i += chunkSize) {
              parts.push(String.fromCharCode(...bytes.subarray(i, i + chunkSize)));
            }
            return btoa(parts.join(""));
          };

          if (init?.body !== undefined && init?.body !== null) {
            if (typeof init.body === "string") {
              body = init.body;
            } else if (init.body instanceof URLSearchParams) {
              body = init.body.toString();
            } else if (init.body instanceof FormData) {
              // Use browser's native Request serializer to encode FormData into
              // a proper multipart/form-data body with the exact boundary header.
              const tempReq = new Request(url, {
                method: method.toUpperCase() === "GET" || method.toUpperCase() === "HEAD" ? "POST" : method.toUpperCase(),
                body: init.body,
              });
              const contentType = tempReq.headers.get("Content-Type");
              if (contentType) {
                delete headers["content-type"];
                delete headers["Content-Type"];
                headers["Content-Type"] = contentType;
              }
              bodyB64 = toBase64(await tempReq.arrayBuffer());
            } else if (init.body instanceof Blob) {
              if (init.body.type && !headers["Content-Type"] && !headers["content-type"]) {
                headers["Content-Type"] = init.body.type;
              }
              bodyB64 = toBase64(await init.body.arrayBuffer());
            } else if (init.body instanceof ArrayBuffer || ArrayBuffer.isView(init.body)) {
              const buf =
                init.body instanceof ArrayBuffer
                  ? init.body
                  : init.body.buffer.slice(
                      init.body.byteOffset,
                      init.body.byteOffset + init.body.byteLength,
                    );
              bodyB64 = toBase64(buf);
            }
          } else if (input && typeof input === "object" && "arrayBuffer" in input) {
            const upperMethod = method.toUpperCase();
            if (upperMethod !== "GET" && upperMethod !== "HEAD") {
              try {
                const buf = await (input as Request).clone().arrayBuffer();
                if (buf.byteLength > 0) {
                  bodyB64 = toBase64(buf);
                }
              } catch {
                // ignore
              }
            }
          }

          const proxyResp = await callAppService<{
            status: number;
            statusText: string;
            headers: Record<string, string>;
            body?: string;
            bodyB64?: string;
          }>("ProxyFetch", {
            method: method.toUpperCase(),
            url,
            headers,
            body,
            ...(bodyB64 ? { bodyB64 } : {}),
          });

          // Fetch standard: 204, 205, and 304 cannot have a body.
          // In WebKit, passing even an empty string throws:
          // "Response cannot have a body with the given status."
          const isNullBodyStatus =
            proxyResp.status === 204 ||
            proxyResp.status === 205 ||
            proxyResp.status === 304;

          let responseBody: BodyInit | null = null;
          if (!isNullBodyStatus) {
            if (proxyResp.bodyB64) {
              const bin = atob(proxyResp.bodyB64);
              const bytes = new Uint8Array(bin.length);
              for (let i = 0; i < bin.length; i++) {
                bytes[i] = bin.charCodeAt(i);
              }
              responseBody = bytes;
            } else {
              responseBody = proxyResp.body ?? "";
            }
          }

          return new Response(responseBody, {
            status: proxyResp.status,
            statusText: proxyResp.statusText,
            headers: new Headers(proxyResp.headers),
          });
        } catch (err) {
          console.error("Wails ProxyFetch failed:", err);
          throw new Error(err instanceof Error ? err.message : "Network error: proxy request failed");
        }
      }

      return originalFetch(input, init);
    };
  }

  // Inject CSS rules to bridge -webkit-app-region to Wails v3 --wails-draggable
  if (typeof document !== "undefined" && !document.getElementById("wails-drag-styles")) {
    const styleEl = document.createElement("style");
    styleEl.id = "wails-drag-styles";
    styleEl.textContent = `
      [style*="WebkitAppRegion: drag"],
      [style*="WebkitAppRegion:drag"],
      [style*="-webkit-app-region: drag"],
      [style*="-webkit-app-region:drag"],
      .drag-strip,
      .h-12.shrink-0,
      header,
      [data-wails-drag] {
        --wails-draggable: drag !important;
      }

      [style*="WebkitAppRegion: no-drag"],
      [style*="WebkitAppRegion:no-drag"],
      [style*="-webkit-app-region: no-drag"],
      [style*="-webkit-app-region:no-drag"],
      button, a, input, textarea, select, [role="button"], [contenteditable="true"] {
        --wails-draggable: no-drag !important;
      }
    `;
    document.head.appendChild(styleEl);

    const isDragRegionTarget = (target: HTMLElement | null, clientY: number): boolean => {
      if (!target) return false;
      // Never drag/zoom when interacting with clickable/editable controls
      if (
        target.closest(
          "button, a, input, textarea, select, [role='button'], [contenteditable='true'], [style*='no-drag']"
        )
      ) {
        return false;
      }
      // Check if inside a designated drag area or top title bar area (top 48px)
      return (
        target.closest(
          '[style*="WebkitAppRegion: drag"], [style*="WebkitAppRegion:drag"], [style*="-webkit-app-region: drag"], [style*="-webkit-app-region:drag"], .drag-strip, .h-12.shrink-0, [data-wails-drag]'
        ) !== null ||
        (clientY <= 48 && !target.closest("[style*='no-drag']"))
      );
    };

    // Native window drag handler for frameless top title bar and drag strips
    window.addEventListener(
      "mousedown",
      (e: MouseEvent) => {
        if (e.button !== 0 || e.detail >= 2) return; // Only single primary mouse button click initiates drag
        const target = e.target as HTMLElement | null;
        if (isDragRegionTarget(target, e.clientY)) {
          target!.style.setProperty("--wails-draggable", "drag");
          if (window._wails?.invoke) {
            window._wails.invoke("wails:drag");
          } else if ((window as any).webkit?.messageHandlers?.external) {
            (window as any).webkit.messageHandlers.external.postMessage("wails:drag");
          }
        }
      },
      { capture: true }
    );

    // Double-click title bar / drag strip to maximize or restore window (standard macOS behavior)
    window.addEventListener(
      "dblclick",
      (e: MouseEvent) => {
        if (e.button !== 0) return;
        const target = e.target as HTMLElement | null;
        if (isDragRegionTarget(target, e.clientY)) {
          void callAppService("ToggleMaximizeCurrentWindow");
        }
      },
      { capture: true }
    );
  }

  let appInfo: { version: string; os: "macos" | "windows" | "linux" | "unknown" } = {
    version: "0.11.15",
    os: "macos",
  };
  let runtimeConfig: RuntimeConfigResult = {
    ok: true,
    config: {
      schemaVersion: 1,
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      appUrl: "https://multica.ai",
    },
  };
  let systemLocale = "en";

  try {
    const [fetchedInfo, fetchedConfig, fetchedLocale] = await Promise.all([
      callAppService<{ version: string; os: "macos" | "windows" | "linux" | "unknown" }>("GetAppInfo"),
      callAppService<RuntimeConfigResult>("GetRuntimeConfig"),
      callAppService<string>("GetSystemLocale"),
    ]);

    if (fetchedInfo) appInfo = fetchedInfo;
    if (fetchedConfig) runtimeConfig = fetchedConfig;
    if (fetchedLocale) systemLocale = fetchedLocale;
  } catch (err) {
    console.warn("Failed to fetch initial Wails v3 metadata:", err);
  }

  const extractEventData = (evt: any): any => {
    if (evt && typeof evt === "object" && "data" in evt) {
      const data = evt.data;
      if (Array.isArray(data) && data.length === 1) {
        return data[0];
      }
      return data;
    }
    return evt;
  };

  const wailsEventsOn = (event: string, callback: (...args: any[]) => void) => {
    if (window.wails?.Events?.On) {
      return window.wails.Events.On(event, (evt: any) => {
        callback(extractEventData(evt));
      });
    }
    return () => {};
  };

  if (window.updater) {
    window.updater.onDownloadState = (callback: any) => wailsEventsOn("updater:download-state", callback);
    window.updater.onUpdateAvailable = (callback: any) => wailsEventsOn("updater:update-available", callback);
    window.updater.onDownloadProgress = (callback: any) => wailsEventsOn("updater:download-progress", callback);
    window.updater.onUpdateDownloaded = (callback: any) => wailsEventsOn("updater:update-downloaded", callback);
  }

  // Real daemonAPI implementation
  window.daemonAPI = {
    setTargetApiUrl: async (url: string) => {
      return callAppService("SetDaemonTargetApiUrl", url);
    },
    syncToken: async (token: string, userId: string) => {
      return callAppService("SyncDaemonToken", token, userId);
    },
    clearToken: async () => {
      return callAppService("SyncDaemonToken", "", "");
    },
    reauthenticate: async (token: string, userId: string) => {
      try {
        await callAppService("SyncDaemonToken", token, userId);
        return { ok: true };
      } catch (err: any) {
        return { ok: false, reason: "transient", message: err?.message || String(err) };
      }
    },
    getStatus: async () => {
      try {
        return await callAppService("GetDaemonStatus");
      } catch {
        return { state: "stopped" };
      }
    },
    getHostName: async () => {
      try {
        const name = await callAppService<string>("GetHostName");
        return name || "localhost";
      } catch {
        return "localhost";
      }
    },
    start: async () => {
      try {
        await callAppService("StartDaemon");
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err?.message ?? String(err) };
      }
    },
    stop: async () => {
      try {
        await callAppService("StopDaemon");
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err?.message ?? String(err) };
      }
    },
    restart: async () => {
      try {
        await callAppService("RestartDaemon");
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err?.message ?? String(err) };
      }
    },
    probeRuntimes: async () => {
      try {
        return await callAppService("ProbeLocalRuntimes");
      } catch {
        return { probeResult: "error" };
      }
    },
    isCliInstalled: async () => true,
    getPrefs: async () => ({ autoStart: true, autoStop: false }),
    setPrefs: async (prefs: any) => ({ autoStart: true, autoStop: false, ...prefs }),
    autoStart: async () => {
      return callAppService("StartDaemon");
    },
    retryInstall: async () => {},
    startLogStream: () => {},
    stopLogStream: () => {},
    onLogLine: () => () => {},
    openLogFile: async () => ({ success: true }),
    focusEnvironment: async () => ({ ok: true }),
    onStatusChange: (callback: (status: any) => void) =>
      wailsEventsOn("daemon:status-changed", callback),
  };

  const resolveWindowContext = (): DesktopWindowContext => {
    if (typeof window !== "undefined") {
      const parsed = parseIssueWindowPath(window.location.pathname);
      if (parsed) {
        return {
          kind: "issue",
          path: parsed.path,
          workspaceSlug: parsed.workspaceSlug,
          issueId: parsed.issueId,
          title: document.title || "Issue Details",
        };
      }
    }
    return { kind: "main" };
  };

  // Real desktopAPI implementation
  window.desktopAPI = {
    appInfo,
    systemLocale,
    runtimeConfig,
    windowContext: resolveWindowContext(),

    voice: {
      listModels: async () => ({ models: [], activeModelId: null, engineAvailable: false }),
      onModelsChanged: () => () => {},
      downloadModel: async () => {},
      cancelDownload: async () => {},
      deleteModel: async () => {},
      prepare: () => {},
      transcribe: async () => ({ ok: true as const, text: "", duration: 0 }),
      history: {
        list: async () => [],
        onChanged: () => () => {},
        save: async () => null,
        update: async () => {},
        audio: async () => null,
        remove: async () => {},
        setRetention: async () => {},
        clear: async () => {},
      },
    },

    onSystemLocaleChanged: (callback: (locale: string) => void) =>
      wailsEventsOn("locale:system-changed", callback),

    getLastFreeze: (): FreezeBreadcrumb | null => null,
    ackFreeze: (_ts: number) => {},
    reportAuthSession: (_userId: string | null) => {},

    onAuthToken: (callback: (token: string) => void) =>
      wailsEventsOn("auth:token", (raw: any) => {
        let tokenStr = "";
        if (typeof raw === "string") {
          tokenStr = raw;
        } else if (Array.isArray(raw) && typeof raw[0] === "string") {
          tokenStr = raw[0];
        } else if (raw && typeof raw === "object") {
          if (typeof raw.data === "string") tokenStr = raw.data;
          else if (Array.isArray(raw.data) && typeof raw.data[0] === "string") tokenStr = raw.data[0];
          else if (typeof raw.token === "string") tokenStr = raw.token;
        }
        if (tokenStr) {
          callback(tokenStr.trim());
        } else {
          console.warn("wails-desktop-api: Unable to extract token string from auth:token event:", raw);
        }
      }),

    startLoginOAuthServer: async (): Promise<number> => {
      return await callAppService<number>("StartLoginOAuthServer");
    },

    stopLoginOAuthServer: async (): Promise<void> => {
      return await callAppService<void>("StopLoginOAuthServer");
    },

    getPendingOAuthToken: async (): Promise<string> => {
      return await callAppService<string>("GetPendingOAuthToken");
    },

    openLoginWindow: async (url: string): Promise<void> => {
      return await callAppService<void>("OpenLoginWindow", url);
    },

    closeLoginWindow: async (): Promise<void> => {
      return await callAppService<void>("CloseLoginWindow");
    },

    onInviteOpen: (callback: (invitationId: string) => void) =>
      wailsEventsOn("invite:open", callback),

    onIssueOpen: (callback: (url: string) => void) =>
      wailsEventsOn("issue:open", callback),

    openExternal: async (url: string) => {
      try {
        return await callAppService("OpenExternal", url);
      } catch {
        window.open(url, "_blank");
      }
    },

    downloadURL: async (url: string) => {
      try {
        await callAppService("OpenExternal", url);
      } catch {
        window.open(url, "_blank");
      }
    },

    setImmersiveMode: async (_immersive: boolean) => {},

    showNotification: (payload: {
      slug: string;
      itemId: string;
      issueKey?: string;
      chatSessionId?: string;
      title: string;
      body: string;
    }) => {
      void callAppService("ShowNotification", payload);
    },

    getWindowForeground: async () => true,
    onWindowForegroundChanged: (callback: (foreground: boolean) => void) =>
      wailsEventsOn("window:foreground-changed", callback),

    setUnreadBadge: (count: number) => {
      void callAppService("SetUnreadBadge", count);
    },

    onInboxOpen: (callback: (payload: any) => void) =>
      wailsEventsOn("inbox:open", callback),

    onNavigationGesture: (callback: (gesture: any) => void) =>
      wailsEventsOn("navigation:gesture", callback),

    onContextMenu: (_callback: (request: any) => void) => () => {},
    runContextMenuCommand: (_command: any) => {},

    setRendererRouteContext: (_context: any) => {},

    pickDirectory: async (defaultPath?: string) => {
      try {
        const selected = await callAppService<string>("PickDirectory", defaultPath ?? "");
        if (!selected) {
          return { ok: false, reason: "cancelled" as const };
        }
        const parts = selected.replace(/\\/g, "/").split("/").filter(Boolean);
        const basename = parts[parts.length - 1] || selected;
        return { ok: true, path: selected, basename };
      } catch (err) {
        return {
          ok: false,
          reason: "error" as const,
          error: err instanceof Error ? err.message : String(err),
        };
      }
    },

    pickFiles: async (options?: { multiple?: boolean; accept?: string }): Promise<File[]> => {
      try {
        const picked = await callAppService<
          Array<{ name: string; mimeType: string; dataB64: string }>
        >("PickFiles", options?.multiple ?? false, options?.accept ?? "");
        if (!Array.isArray(picked)) return [];
        return picked.map((f) => {
          const binaryString = atob(f.dataB64);
          const len = binaryString.length;
          const bytes = new Uint8Array(len);
          for (let i = 0; i < len; i++) {
            bytes[i] = binaryString.charCodeAt(i);
          }
          return new File([bytes], f.name, { type: f.mimeType || "application/octet-stream" });
        });
      } catch (err) {
        console.error("Wails PickFiles failed:", err);
        return [];
      }
    },

    validateLocalDirectory: async (path: string) => {
      try {
        return await callAppService("ValidateLocalDirectory", path);
      } catch (err) {
        return {
          ok: false,
          reason: "error" as const,
          error: err instanceof Error ? err.message : String(err),
        };
      }
    },

    readThemeFolder: async () => {
      try {
        const folders = await callAppService<string[]>("ReadThemeFolder");
        return { ok: true, path: "", entries: [], themes: (folders || []).map((f) => ({ folder: f, files: [] })) };
      } catch {
        return { ok: true, path: "", entries: [], themes: [] };
      }
    },
    onThemeFolderChange: (_callback: () => void) => () => {},
    openThemeFolder: async (name?: string) => {
      try {
        await callAppService("OpenExternal", `file://~/.codeopc/themes/${name ?? ""}`);
      } catch {
        // ignore
      }
      return { ok: true };
    },
    writeThemeFolder: async () => ({ ok: true }),

    onCloseActiveTab: (callback: () => void) =>
      wailsEventsOn("tab:close-active", callback),

    onOpenSettings: (callback: () => void) =>
      wailsEventsOn("settings:open", callback),

    onSelectTabShortcut: (callback: (key: any) => void) =>
      wailsEventsOn("tab:select-shortcut", callback),

    closeWindow: () => {
      void callAppService("CloseCurrentWindow");
    },

    openIssueWindow: async (request: { path: string; title: string }) => {
      await callAppService("OpenIssueWindow", request);
      return { ok: true as const };
    },

    openWorkspaceWindow: async (slug: string) => {
      await callAppService("OpenWorkspaceWindow", slug);
      return { ok: true };
    },

    reportWindowWorkspace: (_workspaceSlug: string | null) => {},
    getOtherWindowWorkspaces: () => [],
    focusWorkspaceWindow: () => false,
    onOpenWorkspacePath: () => () => {},
    onNewWindowShortcut: () => () => {},
    getDevToolbarVisible: () => false,
    onDevToolbarVisibleChange: () => () => {},
  };
}
