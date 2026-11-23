# `code-opc` 接入 `platform-opc` 企业登录解决方案（双端覆盖 · 平台直发零换票）

本文档详细阐述 `code-opc` 接入 `platform-opc` 企业微信扫码登录的完整技术方案。针对企业自建需求，本方案采用**平台直签业务 Token + 双端（Web 网页端 & Desktop 桌面端）零换票直达**的架构设计。

---

## 一、方案核心理念与设计原则

### 1. 为什么能够做到“零换票”？
传统的 OAuth2 流程为了应对第三方外部不可信应用，设计了“`code` 换 `exchange_token`，后端再拿密钥调平台接口换取身份并签发本地 `session_token`”的繁琐链路。

但在当前自建企业体系中，**`platform-opc`（统一认证网关）与自建的 `code-opc Server` 均属于企业内部受信任组件**。
我们采用与 `knowledge-opc` 一脉相承的 **“平台集中签发 + 共享秘钥验签 + 客户端直达”** 模式：
1. 员工在企微扫码或网页免密授权完成后，**`platform-opc` 自身直接签发 `code-opc` 认可的最终业务 JWT**；
2. **桌面端场景**：浏览器直接通过操作系统私有协议链接（`multica://auth/callback?token=<JWT>`）将 Token 瞬间注入桌面客户端；
3. **Web 网页端场景**：平台直接 302 重定向带 Token 回到 Web 回调路由（`https://code.yourdomain.com/auth/callback?token=<JWT>`），前端直接调用已有的 `loginWithToken(token)` 存入状态并写入 Cookie；
4. 无论 Web 还是桌面端，拿到 Token 即代表登录已完成，后续所有请求直接带上该 Token 访问自建 `code-opc` 服务端；
5. **全链路没有任何中转临时票据、没有第二道换票接口、没有任何后端服务间的二次回调握手，一步到位！**

---

## 二、端到端交互时序图（Web 端 与 桌面端 对比）

### 1. 桌面端（Desktop App）登录时序
```
+---------------+           +--------------------+           +---------------------+           +---------------------+
| 员工桌面端     |           | 系统默认浏览器      |           | platform-opc        |           | code-opc 自建服务端 |
| (Wails Native)|           | (企微授权页面)     |           | (企业统一网关)      |           | (私有 Go + PG)      |
+---------------+           +--------------------+           +---------------------+           +---------------------+
        |                              |                                |                                 |
        | 1. 点击【企业微信登录】        |                                |                                 |
        |----------------------------->| 2. 唤起打开平台企微登录页       |                                 |
        |                              |    client_id=code-desktop      |                                 |
        |                              |------------------------------->|                                 |
        |                              | 3. 员工扫码 / 网页免登授权       |                                 |
        |                              |<-------------------------------|                                 |
        |                              |                                |                                 |
        |                              | 4. 平台直接使用共享秘钥         |                                 |
        |                              |    签发 code-opc 业务 JWT      |                                 |
        |                              |                                |                                 |
        |                              | 5. 302 重定向唤起系统协议:      |                                 |
        |                              |    multica://auth/callback?    |                                 |
        |                              |    token=<最终业务JWT>          |                                 |
        |<-----------------------------|                                |                                 |
        | 6. 桌面端直接捕获 Token，      |                                |                                 |
        |    安全保存到钥匙串，登录成功!  |                                |                                 |
        |                              |                                |                                 |
        | 7. 携带 Authorization: Bearer <Token> 访问自建业务接口          |                                 |
        |------------------------------------------------------------------------------------------------>|
        |                                                                                                 | 8. 本地秘钥验签通过
        |<------------------------------------------------------------------------------------------------|    若首次访问自动创建
        | 9. 正常拉取 Issue / Task，直接进入协同工作区                                                      |    用户与部门空间(JIT)
```

### 2. Web 网页端（Browser Web）登录时序
```
+------------------------------------+           +---------------------+           +---------------------+
| 用户浏览器 (Web 客户端)            |           | platform-opc        |           | code-opc 自建服务端 |
| https://code.yourdomain.com        |           | (企业统一网关)      |           | (私有 Go + PG)      |
+------------------------------------+           +---------------------+           +---------------------+
                  |                                         |                                 |
                  | 1. 访问 /login 点击【企业微信登录】       |                                 |
                  |---------------------------------------->|                                 |
                  |    redirect_uri=/auth/callback          |                                 |
                  |    client_id=code-web                   |                                 |
                  |                                         |                                 |
                  | 2. 企微扫码 / 员工企业账号授权通过        |                                 |
                  |                                         |                                 |
                  | 3. 平台使用共享秘钥签发业务 JWT          |                                 |
                  |                                         |                                 |
                  | 4. 302 重定向回 Web 端回调地址:          |                                 |
                  |    /auth/callback?token=<最终业务JWT>    |                                 |
                  |<----------------------------------------|                                 |
                  |                                         |                                 |
                  | 5. Web 前端解析 URL 中的 token 参数:    |                                 |
                  |    - 执行 loginWithToken(token)         |                                 |
                  |    - 写入 multica_token 与 Session Cookie|                                 |
                  |    - 页面平滑路由跳转至 /workspaces 首页|                                 |
                  |                                         |                                 |
                  | 6. 携带 Authorization: Bearer <Token> 访问自建业务接口                          |
                  |-------------------------------------------------------------------------->|
                  |                                                                           | 7. 本地秘钥验签通过
                  |<--------------------------------------------------------------------------|    JIT 自动预配
                  | 8. 正常渲染工作区数据                                                     |
```

---

## 三、关键技术契约规范

### 1. 签名算法与密钥契约
- **算法**：`HS256`（HMAC-SHA256）。
- **共享秘钥**：`platform-opc` 与 `code-opc Server` 在环境变量中配置完全一致的 `JWT_SECRET`（例如：`JWT_SECRET=company-production-opc-token-secret-2026`）。

### 2. 平台签发的 JWT Payload 字段规范
`platform-opc` 在企微扫码确认后，直接签发包含以下标准字段的 JWT：

```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",   // 用户唯一UUID（基于企微 UnionID 或 UserID 生成稳定 UUID）
  "email": "zhangsan@benlai.com",                    // 员工企业邮箱
  "name": "张三",                                    // 企微真实姓名
  "dept": "研发中心",                                // 员工所属主部门/空间名称
  "sid": "c20ad411-2092-4f0e-b789-08b5f39281a1",   // 会话唯一ID (UUID)
  "iat": 1732014000,                                 // 签发时间戳
  "exp": 1734606000                                  // 过期时间戳（例如 30 天长效免登）
}
```

---

## 四、各模块具体改造细节

### 1. `platform-opc`（认证中心分流）
- **改造位置**：`platform-opc/pkg/handler/handler.go` 中的 `OAuthCallback` 方法。
- **改造内容**：
  - 获取来源参数 `client_id` 或 `redirect_uri`：
    - **如果是桌面端（`client_id=code-desktop` 或 `multica://`）**：
      ```go
      targetURL := fmt.Sprintf("multica://auth/callback?token=%s", url.QueryEscape(tokenString))
      c.Redirect(http.StatusFound, targetURL)
      ```
    - **如果是 Web 端（`client_id=code-web`）**：
      ```go
      // redirect_uri 如: https://code.yourdomain.com/auth/callback
      targetURL := fmt.Sprintf("%s?token=%s", redirectURI, url.QueryEscape(tokenString))
      c.Redirect(http.StatusFound, targetURL)
      ```

### 2. `code-opc Web`（网页端轻量适配 · 2处修改）
- **改造位置 1：登录按钮跳转** (`packages/views/auth/login-page.tsx` 或 `apps/web/app/(auth)/login/page.tsx`)
  - 增加【企业微信登录】按钮，点击执行跳转：
    ```ts
    const platformLoginUrl = `${platformUrl}/wecom/login?client_id=code-web&redirect_uri=${encodeURIComponent(window.location.origin + "/auth/callback")}`;
    window.location.href = platformLoginUrl;
    ```
- **改造位置 2：回调接收与落地** (`apps/web/app/auth/callback/page.tsx`)
  - `code-opc` 的 `@multica/core/auth` store 中已经内置封装了 `loginWithToken(token)` 方法。
  - 在 `apps/web/app/auth/callback/page.tsx` 中增加对 `token` 参数的直接消费：
    ```ts
    const token = searchParams.get("token");
    if (token) {
      loginWithToken(token).then((user) => {
        setLoggedInCookie();
        router.push("/workspaces");
      }).catch((err) => {
        setError(callbackErrorFrom(err));
      });
      return;
    }
    ```
  - **优势**：不再经过 `api.googleLogin(code)`，不发起换票接口请求，直接拿 Token 进系统！

### 3. `code-opc Desktop`（桌面端适配）
- **改造位置**：`apps/desktop/`
- **改造内容**：
  - **协议注册**：在 macOS 的 `Info.plist`（`CFBundleURLSchemes = ["multica"]`）及 Windows 注册表中注册 `multica://` 协议；
  - **登录按钮交互**：点击【企业微信登录】时，直接调用系统默认浏览器打开：
    `https://platform.yourdomain.com/wecom/login?client_id=code-desktop`；
  - **协议唤起捕获**：在 Wails 应用生命周期中捕获打开 URL，从中解析出 `token` 参数；
  - **持久化**：将 Token 保存至安全存储，并通过 `loginWithToken(token)` 进入工作区。

### 4. `code-opc Server`（自建业务服务端）
- **改造位置**：`server/internal/middleware/auth.go`
- **改造内容**：
  - **共享秘钥验签**：`auth.JWTSecret()` 自动读取相同的环境变量 `JWT_SECRET`，通过标准 `jwt.Parse` 校验签名；
  - **JIT 免感知预配**：
    - 校验通过后，检查 `users` 表是否存在该 `sub`；
    - 若不存在，自动将该 `sub`、`email`、`name` 插入 `users` 表；
    - 检查是否存在对应的部门 `Workspace`，自动完成创建并将该用户加为成员。
  - **全链路无需向 platform-opc 发送任何同步查询请求**，无论是 Web 端还是桌面端发来的请求，验证逻辑 100% 统一闭环。

---

## 五、方案优势总结

1. **双端完全统一规范**：Web 端和桌面端共用一套企业微信认证源、共用一套 JWT Payload 和验签逻辑。
2. **两端均为绝对零换票**：无论是桌面的私有协议，还是 Web 端的重定向回调，都是平台直签直接消费，零临时票据、零二次回调。
3. **极简开发与复用**：`code-opc` 前端已有 `loginWithToken` 现成能力，Web 端仅需适配跳转与读取参数两处，极低改动风险。
