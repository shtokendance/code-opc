import { useEffect, useState } from "react";
import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useAuthStore } from "@multica/core/auth";

function requireRuntimeAppUrl(): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config.appUrl;
}

export function DesktopLoginPage() {
  const webUrl = requireRuntimeAppUrl();
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

    const checkPendingToken = async () => {
      if (window.desktopAPI?.getPendingOAuthToken) {
        try {
          const tok = await window.desktopAPI.getPendingOAuthToken();
          if (tok && tok.trim()) {
            console.log("[DesktopLoginPage] Retrieved pending OAuth token on window focus");
            await useAuthStore.getState().loginWithToken(tok.trim());
          }
        } catch {
          // ignore
        }
      }
    };

    window.addEventListener("focus", checkPendingToken);
    return () => {
      window.removeEventListener("focus", checkPendingToken);
    };
  }, []);

  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${webUrl}/login?platform=desktop`,
    );
  };

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
    if (port && port > 0) {
      const redirectUri = `http://127.0.0.1:${port}/callback`;
      const loginUrl = `${platformUrl}/wecom/login?client_id=code-desktop&redirect_uri=${encodeURIComponent(redirectUri)}`;
      window.desktopAPI.openExternal(loginUrl);
      return;
    }

    // 兜底方案：通过 multica:// 唤起
    window.desktopAPI.openExternal(
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
        onGoogleLogin={handleGoogleLogin}
        onWeComLogin={handleWeComLogin}
      />
    </div>
  );
}
