import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWSEvent } from "@multica/core/realtime";
import { runtimeKeys, runtimeListOptions } from "@multica/core/runtimes/queries";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";

export interface RuntimeMigration {
  agentId: string;
  fromRuntimeId: string;
  toRuntimeId: string;
}

export interface StaleRuntimePlan {
  migrations: RuntimeMigration[];
  staleRuntimeIds: string[];
}

/**
 * Computes safe automatic migrations from stale offline local runtimes on the
 * same physical machine (matching owner + hostname, different old daemon_id)
 * to the currently online local runtimes for the same provider.
 */
export function planStaleRuntimeReconciliation({
  runtimes,
  agents,
  localDaemonId,
  localMachineName,
  currentUserId,
}: {
  runtimes: AgentRuntime[];
  agents: Agent[];
  localDaemonId: string | null;
  localMachineName: string | null;
  currentUserId: string | null;
}): StaleRuntimePlan {
  if (!localDaemonId || !localMachineName || !currentUserId) {
    return { migrations: [], staleRuntimeIds: [] };
  }

  const onlineByProvider = new Map<string, AgentRuntime>();
  for (const r of runtimes) {
    if (
      r.runtime_mode === "local" &&
      r.status === "online" &&
      r.daemon_id === localDaemonId &&
      r.owner_id === currentUserId &&
      !r.profile_id
    ) {
      onlineByProvider.set(r.provider, r);
    }
  }

  if (onlineByProvider.size === 0) {
    return { migrations: [], staleRuntimeIds: [] };
  }

  const staleRuntimes = runtimes.filter(
    (r) =>
      r.runtime_mode === "local" &&
      r.status === "offline" &&
      r.owner_id === currentUserId &&
      !r.profile_id &&
      !!r.daemon_id &&
      r.daemon_id !== localDaemonId &&
      ((r as any).device_name === localMachineName || (typeof r.metadata?.hostname === "string" && r.metadata.hostname === localMachineName)) &&
      onlineByProvider.has(r.provider),
  );

  if (staleRuntimes.length === 0) {
    return { migrations: [], staleRuntimeIds: [] };
  }

  const migrations: RuntimeMigration[] = [];
  const staleRuntimeIds: string[] = [];

  for (const stale of staleRuntimes) {
    const replacement = onlineByProvider.get(stale.provider);
    if (!replacement) continue;

    staleRuntimeIds.push(stale.id);
    for (const agent of agents) {
      if (!agent.archived_at && agent.runtime_id === stale.id) {
        migrations.push({
          agentId: agent.id,
          fromRuntimeId: stale.id,
          toRuntimeId: replacement.id,
        });
      }
    }
  }

  return { migrations, staleRuntimeIds };
}

export function StaleRuntimeReconciler() {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? null;
  if (!wsId) return null;
  return <WorkspaceStaleRuntimeReconciler wsId={wsId} />;
}

function WorkspaceStaleRuntimeReconciler({ wsId }: { wsId: string }) {
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);

  const [localDaemonId, setLocalDaemonId] = useState<string | null>(null);
  const [localMachineName, setLocalMachineName] = useState<string | null>(null);
  const attemptedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    const apiDaemon = window.daemonAPI;
    if (!apiDaemon) return;
    let active = true;
    const refreshStatus = () => {
      apiDaemon.getStatus?.().then((s) => {
        if (active && s.daemonId) setLocalDaemonId(s.daemonId);
      });
    };
    refreshStatus();
    apiDaemon.getHostName?.().then((name) => {
      if (active) setLocalMachineName(name || null);
    });
    const unsub = apiDaemon.onStatusChange?.((s) => {
      if (s.daemonId) setLocalDaemonId(s.daemonId);
    });
    const timer = setInterval(() => {
      if (!localDaemonId) refreshStatus();
    }, 1500);
    return () => {
      active = false;
      clearInterval(timer);
      unsub?.();
    };
  }, [localDaemonId]);

  useWSEvent("daemon:register", () => {
    void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  });

  useEffect(() => {
    if (!localDaemonId) return;
    void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [localDaemonId, wsId, qc]);

  const enabled = !!userId && !!localDaemonId && !!localMachineName;
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId),
    enabled,
  });
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled,
  });

  useEffect(() => {
    if (!enabled) return;

    const plan = planStaleRuntimeReconciliation({
      runtimes,
      agents,
      localDaemonId,
      localMachineName,
      currentUserId: userId,
    });

    const pendingStaleIds = plan.staleRuntimeIds.filter(
      (id) => !attemptedRef.current.has(`${wsId}:${id}`),
    );
    if (pendingStaleIds.length === 0) return;

    for (const id of pendingStaleIds) {
      attemptedRef.current.add(`${wsId}:${id}`);
    }

    let cancelled = false;
    void (async () => {
      let changed = false;
      for (const staleId of pendingStaleIds) {
        const migrations = plan.migrations.filter(
          (m) => m.fromRuntimeId === staleId,
        );
        let allMigrated = true;
        for (const m of migrations) {
          try {
            await api.updateAgent(m.agentId, { runtime_id: m.toRuntimeId });
            changed = true;
          } catch {
            allMigrated = false;
          }
        }
        if (allMigrated) {
          try {
            await api.deleteRuntime(staleId);
            changed = true;
          } catch {
            // Keep stale runtime if server refuses (e.g. another user's private agent).
          }
        }
      }

      if (!cancelled && changed) {
        await Promise.all([
          qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
          qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) }),
        ]);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [enabled, wsId, runtimes, agents, localDaemonId, localMachineName, userId, qc]);

  return null;
}
