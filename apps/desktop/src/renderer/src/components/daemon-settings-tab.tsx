import { useState, useEffect, useCallback } from "react";
import { AlertCircle, Info, LogIn } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import {
  SettingsCard,
  SettingsRow,
  SettingsTab,
} from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { reauthenticateDaemon } from "../platform/daemon-reauth";
import type { DaemonPrefs, DaemonStatus } from "../../../shared/daemon-types";

export function DaemonSettingsTab() {
  const { t } = useT("settings");
  const [prefs, setPrefs] = useState<DaemonPrefs>({ autoStart: true, autoStop: false });
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [reauthLoading, setReauthLoading] = useState(false);

  useEffect(() => {
    window.daemonAPI.getPrefs().then(setPrefs);
    window.daemonAPI.getStatus().then(setStatus);
    return window.daemonAPI.onStatusChange(setStatus);
  }, []);

  const handleReauth = useCallback(async () => {
    setReauthLoading(true);
    await reauthenticateDaemon(t);
    setReauthLoading(false);
  }, [t]);

  const updatePref = useCallback(
    async (key: keyof DaemonPrefs, value: boolean) => {
      setSaving(true);
      try {
        const updated = await window.daemonAPI.setPrefs({ [key]: value });
        setPrefs(updated);
        toast.success(t(($) => $.desktop.daemon.settings_saved), {
          id: "settings-auto-save",
        });
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : t(($) => $.desktop.daemon.settings_save_failed),
        );
      } finally {
        setSaving(false);
      }
    },
    [t],
  );

  // The daemon runs somewhere the app can't drive (e.g. inside WSL2 behind a
  // Windows desktop): /health is reachable but the lifecycle CLI can't reach
  // its process. Auto-start/auto-stop can't work, so disable them and say why
  // rather than letting the toggles silently no-op. See #3916.
  const externallyManaged = status.externallyManaged === true;

  return (
    <SettingsTab
      title={t(($) => $.desktop.daemon.title)}
    >

      {status.state === "auth_expired" && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div className="min-w-0 flex-1">
            <p className="text-body font-medium text-destructive">
              {t(($) => $.desktop.daemon.signin_expired)}
            </p>
            <p className="mt-0.5 text-body text-muted-foreground">
              {t(($) => $.desktop.daemon.signin_expired_description)}
            </p>
          </div>
          <Button
            size="sm"
            className="shrink-0"
            onClick={handleReauth}
            disabled={reauthLoading}
          >
            <LogIn className="size-3.5 mr-1.5" />
            {t(($) => $.desktop.daemon.signin_again)}
          </Button>
        </div>
      )}

      {externallyManaged && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border bg-muted/30 px-4 py-3">
          <Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
          <p className="min-w-0 text-body text-muted-foreground">
            {t(($) => $.desktop.daemon.external_description_before)}{" "}
            <code className="font-mono text-caption">multica daemon start</code> /{" "}
            <code className="font-mono text-caption">multica daemon stop</code>
            {t(($) => $.desktop.daemon.external_description_after)}
          </p>
        </div>
      )}

      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.daemon.auto_start_title)}
          description={t(($) => $.desktop.daemon.auto_start_description)}
        >
          <Switch
            checked={prefs.autoStart}
            onCheckedChange={(checked) => updatePref("autoStart", checked)}
            disabled={saving || externallyManaged}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.daemon.auto_stop_title)}
          description={t(($) => $.desktop.daemon.auto_stop_description)}
        >
          <Switch
            checked={prefs.autoStop}
            onCheckedChange={(checked) => updatePref("autoStop", checked)}
            disabled={saving || externallyManaged}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsTab>
  );
}
