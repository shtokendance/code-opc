import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";

export function DesktopLoginPage() {
  const qc = useQueryClient();
  const [oauthPort, setOauthPort] = useState<number | null>(null);

  // 客户端加载登录的时候打开一个本地端口，等待登录信息回传
  useEffect(() => {
    if (window.desktopAPI?.startLoginOAuthServer) {
      window.desktopAPI
        .startLoginOAuthServer()
        .then((port) => {
          console.log("[DesktopLoginPage] OAuth loopback port listening on:", port);
          setOauthPort(port);
        })
        .catch((err) => {
          console.error("Failed to start login OAuth server:", err);
        });
    }

    let isSubscribed = true;

    const handleReceivedToken = async (tok: string) => {
      const cleanToken = tok.trim();
      if (!cleanToken || !isSubscribed) return;
      console.log("[DesktopLoginPage] Processing received OAuth token");
      try {
        await useAuthStore.getState().loginWithToken(cleanToken);
        window.desktopAPI?.closeLoginWindow?.().catch(() => {});
        try {
          const wsList = await api.listWorkspaces();
          qc.setQueryData(workspaceKeys.list(), wsList);
        } catch (wsErr) {
          console.warn("[DesktopLoginPage] Failed to prefetch workspaces:", wsErr);
        }
      } catch (err) {
        console.error("[DesktopLoginPage] loginWithToken error:", err);
        const msg = err instanceof Error ? err.message : String(err);
        const apiUrl = window.desktopAPI?.runtimeConfig?.ok
          ? window.desktopAPI.runtimeConfig.config.apiUrl
          : "unknown";
        toast.error(`登录验证失败 (${apiUrl}): ${msg}`);
      }
    };

    const tryConsumeToken = async () => {
      if (!isSubscribed || !window.desktopAPI?.getPendingOAuthToken) return;
      try {
        const tok = await window.desktopAPI.getPendingOAuthToken();
        if (tok && tok.trim() && isSubscribed) {
          await handleReceivedToken(tok);
        }
      } catch (err) {
        console.error("[DesktopLoginPage] Error consuming pending OAuth token:", err);
      }
    };

    // 1. 每 800ms 主动轮询检查本地端口是否接收到了 Token（避免后台 WebView 事件挂起或被系统休眠截断）
    const intervalId = setInterval(tryConsumeToken, 800);

    // 2. 窗口重获焦点时立即检查
    window.addEventListener("focus", tryConsumeToken);

    // 3. 同时直接监听 Wails auth:token 事件
    const unsubEvent = window.desktopAPI?.onAuthToken
      ? window.desktopAPI.onAuthToken(async (token) => {
          if (token && typeof token === "string" && token.trim() && isSubscribed) {
            console.log("[DesktopLoginPage] onAuthToken event received in LoginPage");
            await handleReceivedToken(token);
          }
        })
      : () => {};

    return () => {
      isSubscribed = false;
      clearInterval(intervalId);
      window.removeEventListener("focus", tryConsumeToken);
      unsubEvent();
    };
  }, [qc]);

  const handleWeComLogin = async () => {
    let port = oauthPort;
    if (!port && window.desktopAPI?.startLoginOAuthServer) {
      try {
        port = await window.desktopAPI.startLoginOAuthServer();
        setOauthPort(port);
      } catch (err) {
        console.error("Start login OAuth server failed on click:", err);
      }
    }

    const platformUrl = "https://shtokendance.benlai.com";
    const openLoginTarget = (targetUrl: string) => {
      if (window.desktopAPI?.openLoginWindow) {
        const returnUrl = `${window.location.origin}/`;
        return window.desktopAPI.openLoginWindow(targetUrl, returnUrl);
      }
      return window.desktopAPI.openExternal(targetUrl);
    };

    if (port && port > 0) {
      const redirectUri = `http://127.0.0.1:${port}/callback`;
      const loginUrl = `${platformUrl}/wecom/login?client_id=code-desktop&redirect_uri=${encodeURIComponent(redirectUri)}`;
      await openLoginTarget(loginUrl);
      return;
    }

    // 兜底方案：通过 codeopc:// / multica:// 唤起
    await openLoginTarget(
      `${platformUrl}/wecom/login?client_id=code-desktop&state=code-desktop`,
    );
  };

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<MulticaIcon mode="intro" size={48} />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        onWeComLogin={handleWeComLogin}
        hideEmailInput={true}
      />
    </div>
  );
}
