// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const context = vi.hoisted(() => ({
  handlers: new Map<string, (...args: unknown[]) => unknown>(),
  exec: vi.fn(),
  health: vi.fn(),
  readPort: vi.fn(),
  exists: vi.fn(),
  install: vi.fn(),
  syncCliSelfHostConfig: vi.fn().mockResolvedValue(true),
  ensureCliInPath: vi.fn().mockResolvedValue({ installed: true, reason: "installed" }),
  autoStart: true,
  autoStop: false,
  locale: "en",
  windows: [] as Array<{
    isDestroyed: () => boolean;
    isVisible: () => boolean;
    hide: () => void;
    show: () => void;
  }>,
}));

vi.mock("electron", () => ({
  app: {
    getAppPath: () => "/test/desktop",
    getPreferredSystemLanguages: () => [context.locale],
    on: vi.fn(),
    quit: vi.fn(),
    emit: vi.fn(),
  },
  dialog: { showMessageBox: vi.fn() },
  BrowserWindow: { getAllWindows: () => context.windows },
  ipcMain: {
    handle: (name: string, handler: (...args: unknown[]) => unknown) => {
      context.handlers.set(name, handler);
    },
    on: vi.fn(),
  },
}));

vi.mock("child_process", () => ({ execFile: context.exec }));
vi.mock("fs", async (original) => ({
  ...((await original<typeof import("fs")>()) as object),
  existsSync: context.exists,
}));

vi.mock("fs/promises", async (original) => ({
  ...((await original<typeof import("fs/promises")>()) as object),
  readFile: vi.fn(async (path: string) => {
    if (path.endsWith("daemon.pid")) {
      throw Object.assign(new Error("No PID file"), { code: "ENOENT" });
    }
    return path.endsWith("desktop_prefs.json")
      ? JSON.stringify({ autoStart: context.autoStart, autoStop: context.autoStop })
      : JSON.stringify({ server_url: "https://example.test" });
  }),
  writeFile: vi.fn(),
  mkdir: vi.fn(),
}));

vi.mock("./cli-bootstrap", () => ({
  ensureManagedCli: context.install,
  managedCliPath: () => "/test/managed/multica",
}));

vi.mock("./daemon-port", () => ({
  probeProfileHealth: context.health,
  readDaemonPortRecord: context.readPort,
}));

vi.mock("./cli-config-sync", () => ({
  syncCliSelfHostConfig: context.syncCliSelfHostConfig,
}));

vi.mock("./cli-path-installer", () => ({
  ensureCliInPath: context.ensureCliInPath,
}));

describe("daemon manager bootstrap and auto-start lifecycle", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    vi.useFakeTimers();
    context.handlers.clear();
    context.autoStart = true;
    context.autoStop = false;
    context.readPort.mockResolvedValue(null);
    context.exists.mockReturnValue(true);
    context.health.mockResolvedValue(null); // daemon is not running initially
    context.exec.mockImplementation(
      (
        _bin: string,
        args: string[],
        _options: unknown,
        callback: (error: Error | null, output?: string) => void,
      ) => {
        if (args[0] === "version") {
          callback(null, '{"version":"test"}');
        } else if (args[1] === "start") {
          callback(null);
        } else {
          callback(null);
        }
      },
    );
    vi.spyOn(console, "log").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("syncs self-host config to cli and starts daemon when prefs.autoStart is true", async () => {
    const { setupDaemonManager } = await import("./daemon-manager");
    const runtimeConfigGetter = vi.fn().mockReturnValue({
      schemaVersion: 1,
      apiUrl: "https://self-host.test/api",
      wsUrl: "wss://self-host.test/ws",
      appUrl: "https://self-host.test",
    });

    setupDaemonManager(() => null, () => [], {
      runtimeConfigGetter,
    });

    // Wait for async background bootstrap to run
    await vi.waitFor(() => {
      expect(context.syncCliSelfHostConfig).toHaveBeenCalledWith({
        apiUrl: "https://self-host.test/api",
        appUrl: "https://self-host.test",
      });
    });

    expect(context.ensureCliInPath).toHaveBeenCalled();

    // Verify daemon start was triggered
    await vi.waitFor(() => {
      expect(
        context.exec.mock.calls.filter(([, args]) => args[1] === "start"),
      ).toHaveLength(1);
    });
  });

  it("does not start daemon when prefs.autoStart is false", async () => {
    context.autoStart = false;
    const { setupDaemonManager } = await import("./daemon-manager");
    const runtimeConfigGetter = vi.fn().mockReturnValue({
      schemaVersion: 1,
      apiUrl: "https://self-host.test/api",
      wsUrl: "wss://self-host.test/ws",
      appUrl: "https://self-host.test",
    });

    setupDaemonManager(() => null, () => [], {
      runtimeConfigGetter,
    });

    await vi.waitFor(() => {
      expect(context.syncCliSelfHostConfig).toHaveBeenCalled();
    });

    // Wait a turn to ensure startDaemon was not invoked
    await vi.advanceTimersByTimeAsync(1000);
    expect(
      context.exec.mock.calls.filter(([, args]) => args[1] === "start"),
    ).toHaveLength(0);
  });

  it("does not start daemon if already running", async () => {
    context.autoStart = true;
    context.health.mockResolvedValue({ status: "running", active_task_count: 0 });

    const { setupDaemonManager } = await import("./daemon-manager");
    const runtimeConfigGetter = vi.fn().mockReturnValue({
      schemaVersion: 1,
      apiUrl: "https://self-host.test/api",
      wsUrl: "wss://self-host.test/ws",
      appUrl: "https://self-host.test",
    });

    setupDaemonManager(() => null, () => [], {
      runtimeConfigGetter,
    });

    await vi.waitFor(() => {
      expect(context.syncCliSelfHostConfig).toHaveBeenCalled();
    });

    await vi.advanceTimersByTimeAsync(1000);
    expect(
      context.exec.mock.calls.filter(([, args]) => args[1] === "start"),
    ).toHaveLength(0);
  });

  it("proceeds normally when runtimeConfigGetter is omitted", async () => {
    const { setupDaemonManager } = await import("./daemon-manager");

    setupDaemonManager(() => null);

    // Wait for bootstrap
    await vi.waitFor(() => {
      expect(context.ensureCliInPath).toHaveBeenCalled();
    });

    expect(context.syncCliSelfHostConfig).not.toHaveBeenCalled();
  });
});
