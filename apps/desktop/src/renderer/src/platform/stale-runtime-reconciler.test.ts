// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { planStaleRuntimeReconciliation } from "./stale-runtime-reconciler";

function runtime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    name: "Claude (MacBook)",
    runtime_mode: "local",
    provider: "claude",
    status: "online",
    daemon_id: "daemon-new",
    device_name: "MacBook.local",
    owner_id: "user-1",
    ...overrides,
  } as AgentRuntime;
}

function agent(overrides: Partial<Agent>): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    name: "Developer",
    runtime_id: "rt-old",
    ...overrides,
  } as Agent;
}

describe("planStaleRuntimeReconciliation", () => {
  it("migrates active agents from stale offline same-machine runtimes to online replacements", () => {
    const plan = planStaleRuntimeReconciliation({
      localDaemonId: "daemon-new",
      localMachineName: "MacBook.local",
      currentUserId: "user-1",
      runtimes: [
        runtime({ id: "rt-new-claude", provider: "claude", status: "online", daemon_id: "daemon-new" }),
        runtime({ id: "rt-old-claude", provider: "claude", status: "offline", daemon_id: "daemon-old" }),
        runtime({ id: "rt-other-user", provider: "claude", status: "offline", daemon_id: "daemon-other", owner_id: "user-2" }),
        runtime({ id: "rt-other-host", provider: "claude", status: "offline", daemon_id: "daemon-pc", metadata: { hostname: "OtherPC" } }),
      ],
      agents: [
        agent({ id: "a-active", runtime_id: "rt-old-claude" }),
        agent({ id: "a-archived", runtime_id: "rt-old-claude", archived_at: "2026-01-01T00:00:00Z" }),
      ],
    });

    expect(plan.staleRuntimeIds).toEqual(["rt-old-claude"]);
    expect(plan.migrations).toEqual([
      { agentId: "a-active", fromRuntimeId: "rt-old-claude", toRuntimeId: "rt-new-claude" },
    ]);
  });

  it("does nothing when no online runtime is registered on the local daemon", () => {
    const plan = planStaleRuntimeReconciliation({
      localDaemonId: "daemon-new",
      localMachineName: "MacBook.local",
      currentUserId: "user-1",
      runtimes: [
        runtime({ id: "rt-old-claude", provider: "claude", status: "offline", daemon_id: "daemon-old" }),
      ],
      agents: [agent({ id: "a-1", runtime_id: "rt-old-claude" })],
    });

    expect(plan.staleRuntimeIds).toEqual([]);
    expect(plan.migrations).toEqual([]);
  });
});
