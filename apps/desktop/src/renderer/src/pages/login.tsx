import { useState } from "react";
import { useT } from "@multica/views/i18n";
import { DragStrip } from "@multica/views/platform";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import {
  AlertCircle,
  Sparkles,
  ShieldCheck,
  Building2,
} from "@multica/ui/icons";
import {
  beginEnterpriseLogin,
  EnterpriseLoginError,
  exchangeEnterpriseCode,
  runEnterpriseLogin,
} from "../platform/enterprise-login";
import { establishEnterpriseSession } from "../platform/enterprise-session";

export function DesktopLoginPage() {
  const { t } = useT("auth");
  const apiUrl = window.desktopAPI.runtimeConfig.ok
    ? window.desktopAPI.runtimeConfig.config.apiUrl
    : "";

  const [enterprisePending, setEnterprisePending] = useState(false);
  const [enterpriseError, setEnterpriseError] = useState<string | null>(null);

  const handleEnterpriseLogin = async () => {
    if (enterprisePending) return;
    setEnterprisePending(true);
    setEnterpriseError(null);
    try {
      const session = await runEnterpriseLogin({
        beginAuth: () => window.enterpriseAPI.beginAuth(),
        cancelAuth: () => window.enterpriseAPI.cancelAuth(),
        onAuthCallback: (callback) =>
          window.enterpriseAPI.onAuthCallback(callback),
        openExternal: (url) => window.desktopAPI.openExternal(url),
        exchange: (input) => exchangeEnterpriseCode(apiUrl, input),
        begin: (input) => beginEnterpriseLogin(apiUrl, input),
      });
      // Publishes the session and seals the token in the main-process vault.
      await establishEnterpriseSession(session.token, session.user);
      // Auth store update triggers AppContent re-render → shows DesktopShell.
    } catch (err) {
      if (err instanceof EnterpriseLoginError) {
        setEnterpriseError(err.message);
      } else {
        setEnterpriseError(
          err instanceof Error
            ? err.message
            : t(($) => $.desktop.enterprise.failed),
        );
      }
    } finally {
      setEnterprisePending(false);
    }
  };

  const handleCancelAuth = async () => {
    try {
      await window.enterpriseAPI.cancelAuth();
    } catch {}
    setEnterprisePending(false);
  };

  return (
    <div className="flex h-screen flex-col bg-background text-foreground select-none">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center p-8">
        <div className="w-full max-w-sm space-y-8 text-center">
          {/* Logo & Brand Header */}
          <div className="flex flex-col items-center space-y-3">
            <div className="flex size-16 items-center justify-center rounded-2xl bg-primary/10 text-primary shadow-sm ring-1 ring-primary/20">
              <MulticaIcon size={36} />
            </div>
            <div className="space-y-1">
              <h1 className="text-2xl font-bold tracking-tight">TokenDance Code</h1>
              <p className="text-sm text-muted-foreground">企业专属协同研发平台</p>
            </div>
          </div>

          {/* Login Card */}
          <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
            {enterprisePending ? (
              <div className="flex flex-col items-center space-y-4 py-4">
                <Spinner className="size-8 text-primary animate-spin" />
                <div className="space-y-1 text-center">
                  <p className="text-sm font-medium">正在调起浏览器完成授权...</p>
                  <p className="text-xs text-muted-foreground">
                    请在弹出的系统浏览器中完成 TokenDance 账号认证，认证成功后将自动返回本客户端。
                  </p>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => void handleCancelAuth()}
                  className="mt-2 text-xs text-muted-foreground hover:text-foreground"
                >
                  取消认证
                </Button>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="space-y-1 text-left">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    <Building2 className="size-4 text-primary" />
                    <span>统一单点登录 (SSO)</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    已启用 TokenDance 平台统一认证，无需输入密码，点击下方按钮一键完成企业账号授权。
                  </p>
                </div>

                {enterpriseError && (
                  <div className="flex items-start gap-2 rounded-lg bg-destructive/10 p-3 text-left text-xs text-destructive ring-1 ring-destructive/20">
                    <AlertCircle className="size-4 shrink-0 mt-0.5" />
                    <div className="flex-1">
                      <p className="font-semibold">登录未完成</p>
                      <p className="mt-0.5">{enterpriseError}</p>
                    </div>
                  </div>
                )}

                <Button
                  type="button"
                  className="w-full flex items-center justify-center gap-2 font-medium"
                  size="lg"
                  onClick={() => void handleEnterpriseLogin()}
                >
                  <Sparkles className="size-4" />
                  <span>使用 TokenDance 账号登录</span>
                </Button>
              </div>
            )}
          </div>

          {/* Security / System Footer */}
          <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
            <ShieldCheck className="size-3.5 text-emerald-500" />
            <span>受企业安全合规保护的会话管理</span>
          </div>
        </div>
      </div>
    </div>
  );
}
