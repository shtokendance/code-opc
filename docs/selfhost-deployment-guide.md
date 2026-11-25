# `code-opc` 服务端与 Web 端配置及部署指南

本文档基于 `code-opc` 仓库中的 `Dockerfile`、`Dockerfile.web`、`docker-compose.selfhost.yml` 与 `docker-compose.selfhost.build.yml`，说明服务端（Go Backend）与 Web 端（Next.js Frontend）的服务架构、环境变量配置以及在**已有 PostgreSQL 与 Redis** 场景下的完整部署方案。

---

## 一、 整体服务架构

```text
+-----------------------+       +-----------------------+       +-------------------------------+
|  Desktop 客户端 (Wails)|       |  Web 浏览器            |       |  统一认证网关 (platform-opc)   |
|  macOS / Windows      |       |  :3080                |       |  weknora-gateway (:3002)      |
+-----------+-----------+       +-----------+-----------+       +---------------+---------------+
            |                               |                                   |
            | HTTP/WS (:3088)               | HTTP (:3080)                      | 企微 OAuth 直签 JWT
            |                               v                                   | (共享 JWT_SECRET)
            |                   +-----------+-----------+                       |
            |                   |   multica-frontend    |<----------------------+
            |                   |   Next.js (:3000)     |
            |                   |   代理 /api -> :8080   |
            |                   +-----------+-----------+
            |                               |
            v                               v
+-----------+-------------------------------+-----------+
|                    multica-backend                    |
|              Go (Chi + sqlc + WebSocket)              |
|                 容器内 :8080 / 宿主机 :3088             |
+------+--------------------+--------------------+------+
       |                    |                    |
       v                    v                    v
+------+------+      +------+------+      +------+------------------+
| PostgreSQL  |      |    Redis    |      | MinIO (S3)              |
| pgvector    |      |             |      | 附件与头像对象存储        |
+-------------+      +-------------+      +-------------------------+
```

### 镜像构建说明

1. **服务端（`Dockerfile`）**
   - 构建阶段（`golang:1.27.4-alpine`）：编译生成 `server`（主 API/WS 服务）、`migrate`（数据库迁移）、`multica`（CLI）、`maintenance` 等二进制。
   - 运行阶段（`alpine:3.21`）：将二进制、`server/migrations/` 和 `docker/entrypoint.sh` 打包进 `/app`，暴露 `8080` 端口。
   - 启动流程（`./entrypoint.sh`）：启动时先执行 `./migrate up` 自动完成数据库迁移，随后执行 `exec ./server`。

2. **Web 端（`Dockerfile.web`）**
   - 构建阶段（`node:22.23.4-alpine`）：使用 `pnpm install --frozen-lockfile` 安装工作区依赖，设置 `STANDALONE=true` 执行 `pnpm --filter @multica/web build`。
   - 运行阶段：复制 `.next/standalone`、`.next/static` 与 `apps/web/public` 至 `/app`，以 `node apps/web/server.js` 启动，暴露 `3000` 端口。

---

## 二、 核心环境变量配置（`.env`）

在部署根目录执行 `cp .env.example .env` 后，按需配置以下变量：

### 1. 服务端（`backend`）核心配置

| 变量名 | 必填 | 说明与示例 |
| --- | --- | --- |
| `DATABASE_URL` | **是** | 已有 PostgreSQL（需启用 `pgvector` 扩展，且已创建库）连接串，如 `postgres://root:密码@db:5432/opc_code?sslmode=disable` |
| `REDIS_URL` | 推荐 | 已有 Redis 连接串，如 `redis://:密码@redis:6379/4` |
| `REDIS_CLUSTER_MODE` | 否 | 单机 Redis 设为 `false` |
| `JWT_SECRET` | **是** | JWT 签名共享密钥（需与 `platform-opc` 保持完全一致，可用 `openssl rand -hex 32` 生成） |
| `PORT` | 否 | 容器内监听端口，默认 `8080`；宿主机映射端口通过 `BACKEND_PORT`（如 `3088`）控制 |
| `FRONTEND_ORIGIN` | **是** | Web 端访问地址（用于 CORS 与回调校验），如 `http://10.101.102.116:3080` |
| `MULTICA_APP_URL` | **是** | 应用对外基础 URL，如 `http://10.101.102.116:3080` |
| `ALLOW_SIGNUP` | 否 | 是否允许首次登录自动注册用户（企业 SSO/JIT 建议设为 `"true"`） |
| `DISABLE_WORKSPACE_CREATION` | 否 | 设为 `"true"` 可禁止普通用户自行新建工作区（仅由部门同步/JIT 预配） |
| `S3_BUCKET` / `AWS_ENDPOINT_URL` / `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `S3_USE_PATH_STYLE` | 按需 | 对象存储配置（对接 MinIO 需设 `S3_USE_PATH_STYLE=true`），用于附件与头像存储 |
| `PLATFORM_GATEWAY_URL` / `PLATFORM_SERVICE_TOKEN` | 企业版 | 对接企业网关（如 `http://weknora-gateway:3002`）与内部服务鉴权 Token |

### 2. Web 端（`frontend`）核心配置

| 变量名 | 说明与示例 |
| --- | --- |
| `HOSTNAME` | 设为 `"0.0.0.0"`，保证容器内可被宿主机转发访问 |
| `REMOTE_API_URL` | Next.js 服务端反向代理 `/api/*` 的后端内网地址，如 `http://multica-backend:8080` |
| `NEXT_PUBLIC_API_URL` | 浏览器端请求的 API 地址；若走前端同源反代可设为空或前端地址（如 `http://10.101.102.116:3080`） |
| `NEXT_PUBLIC_WS_URL` | WebSocket 地址；留空时默认与当前页面同源 |
| `FRONTEND_PORT` | 宿主机映射端口（默认 `3000`，生产环境可设为 `3080`） |

---

## 三、 已有 PostgreSQL 与 Redis 的源码构建部署（推荐）

由于仓库自带的 `docker-compose.selfhost.yml` 内置了 `postgres` 容器且 `backend` 强依赖 `depends_on: postgres`，在**已有 PostgreSQL 和 Redis** 的环境下，建议在代码根目录新建独立的编排文件 `docker-compose.prod.yml`，直接从源码构建 `backend` 与 `frontend`：

### 1. 准备 `.env` 文件

```env
# 1. 数据库与缓存（指向已有 PostgreSQL 和 Redis）
# 若 PG/Redis 在同一个 Docker 网络中，主机名直接填容器名（如 db / redis）
# 若在宿主机或独立机器上，填内网 IP
DATABASE_URL=postgres://root:你的密码@db:5432/opc_code?sslmode=disable
REDIS_URL=redis://:你的密码@redis:6379/4
REDIS_CLUSTER_MODE=false

# 2. 安全与认证（需与 platform-opc 保持完全一致）
JWT_SECRET=你的JWT密钥
ALLOW_SIGNUP=true
DISABLE_WORKSPACE_CREATION=true

# 3. 端口与访问地址
BACKEND_PORT=3088
FRONTEND_PORT=3080
FRONTEND_ORIGIN=http://你的服务器IP:3080
MULTICA_APP_URL=http://你的服务器IP:3080

# 4. Web 前端代理配置
REMOTE_API_URL=http://multica-backend:8080
NEXT_PUBLIC_API_URL=http://你的服务器IP:3080
NEXT_PUBLIC_WS_URL=

# 5. 对象存储（MinIO / S3，按需填写）
S3_BUCKET=multica
S3_REGION=us-east-1
AWS_ENDPOINT_URL=http://tokendance-knowledge-minio:9000
S3_USE_PATH_STYLE=true
AWS_ACCESS_KEY_ID=你的AK
AWS_SECRET_ACCESS_KEY=你的SK
```

### 2. 创建 `docker-compose.prod.yml`

```yaml
name: multica

services:
  backend:
    container_name: multica-backend
    build:
      context: .
      dockerfile: Dockerfile
      args:
        VERSION: ${VERSION:-0.11.6}
    ports:
      - "0.0.0.0:${BACKEND_PORT:-3088}:8080"
    volumes:
      - backend_uploads:/app/data/uploads
      - /data/releases:/data/releases   # 桌面端热更新包目录（按需）
    env_file:
      - .env
    environment:
      PORT: "8080"
      APP_ENV: production
    networks:
      - root_default
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8080/api/config || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5

  frontend:
    container_name: multica-frontend
    build:
      context: .
      dockerfile: Dockerfile.web
      args:
        NEXT_PUBLIC_APP_VERSION: ${VERSION:-0.11.6}
    depends_on:
      backend:
        condition: service_healthy
    ports:
      - "0.0.0.0:${FRONTEND_PORT:-3080}:3000"
    env_file:
      - .env
    environment:
      HOSTNAME: "0.0.0.0"
      REMOTE_API_URL: http://multica-backend:8080
    networks:
      - root_default
    restart: unless-stopped

networks:
  root_default:
    external: true   # 接入已有 PG/Redis 所在的 Docker 网络；若不在 Docker 网络中，可删除 networks 段并直接走内网 IP

volumes:
  backend_uploads:
```

### 3. 构建并启动

```bash
# 首次构建并启动（启动时会自动执行数据库 migrations）
docker compose -f docker-compose.prod.yml up -d --build

# 查看日志确认数据库迁移及服务启动状态
docker compose -f docker-compose.prod.yml logs -f
```

后续代码更新升级：

```bash
git pull origin release
docker compose -f docker-compose.prod.yml up -d --build
```

---

## 四、 轻量热更新模式（仅更新服务端二进制）

如果仅修改了 Go 服务端代码且容器已将宿主机二进制挂载至 `/app/server`（例如 `/data/multica/server/multica-server-linux:/app/server`），可直接交叉编译后替换文件并重启容器，无需重建镜像：

```bash
cd server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server ecs-user@<服务器IP>:/tmp/server-codeopc
ssh ecs-user@<服务器IP> "sudo cp /tmp/server-codeopc /data/multica/server/multica-server-linux && sudo chmod +x /data/multica/server/multica-server-linux && sudo docker restart multica-backend"
```

---

## 五、 部署后验证

1. **检查容器状态**：
   ```bash
   docker ps
   # multica-backend 应显示 healthy（健康检查地址：http://127.0.0.1:8080/api/config）
   ```
2. **验证接口与页面**：
   - 后端配置接口：`curl http://<服务器IP>:3088/api/config`
   - Web 前端访问：`http://<服务器IP>:3080`
3. **查看运行日志**：
   ```bash
   docker logs -f --tail 100 multica-backend
   docker logs -f --tail 100 multica-frontend
   ```
