/**
 * Pure decision logic for what the daemon profile's config.json may receive
 * during a token sync. Extracted from daemon-manager so the gate
 * is unit-testable without touching the filesystem or the network.
 *
 * Both enterprise and personal sessions provision a daemon PAT via `POST /api/tokens`
 * (or pass a `mul_` token straight through), reusing the cached PAT only for the
 * same user, otherwise minting a fresh one on user switch or initial startup.
 */

export type DaemonTokenPlan =
  | { action: "write"; token: string }
  | { action: "mint" };

export interface PlanDaemonTokenInput {
  /** Credential the renderer presented for this session. */
  tokenFromRenderer: string;
  /** Current `token` value in the profile config, if any. */
  cachedToken: unknown;
  /** User the cached PAT was minted for; null when unknown. */
  previousUserId: string | null;
  /** User the current session belongs to. */
  userId: string;
  /** Optional flag preserved for backward compatibility. */
  enterprise?: boolean;
}

export function planDaemonTokenSync(
  input: PlanDaemonTokenInput,
): DaemonTokenPlan {
  if (input.tokenFromRenderer.startsWith("mul_")) {
    return { action: "write", token: input.tokenFromRenderer };
  }

  const userChanged =
    Boolean(input.previousUserId) && input.previousUserId !== input.userId;
  const sameUserWithCachedPat =
    !userChanged &&
    input.previousUserId === input.userId &&
    typeof input.cachedToken === "string" &&
    (input.cachedToken as string).startsWith("mul_");
  if (sameUserWithCachedPat) {
    return { action: "write", token: input.cachedToken as string };
  }

  return { action: "mint" };
}

/** Whether this sync should restart a live daemon for a user switch. */
export function shouldRestartDaemonAfterUserSwitch(
  previousUserId: string | null,
  userId: string,
): boolean {
  return Boolean(previousUserId) && previousUserId !== userId;
}
