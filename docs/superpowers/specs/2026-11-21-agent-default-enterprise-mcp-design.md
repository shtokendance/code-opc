# 智能体默认获取企业已拥有 MCP 设计规范

## 1. 目标与背景
在当前架构中，用户登录部门工作区后，Agent 的 MCP 管理页面（`McpConfigTab`）仅展示本地数据库中的配置，默认为空。而企业平台（`platform-opc` / WeKnora）已经基于用户的企业微信身份与部门租户授权了一批企业 MCP 服务与工具（如知识库检索、业务系统操作工具等）。

本规范的目标是实现：
1. **默认获取已拥有 MCP**：当进入部门工作区查看任意 Agent 的「MCP 管理」时，默认自动拉取并呈现该用户/部门已拥有的企业 MCP 服务及工具，且**默认处于激活/启用状态**。
2. **动态聚合零数据漂移（方案 A）**：不采用本地数据库同步落库方案，而是在查询接口层由服务端动态聚合，平台增删工具或调整权限实时生效。
3. **安全透明执行**：Agent 执行任务调用工具时，通过本地内置的 `remote_mcp_broker` 将调用安全转发给 `platform-opc` 的 `/integration/v1/mcp/invoke-task` 接口执行，整个过程对上游数据库/凭据零明文暴露。

---

## 2. 系统架构设计

```
[前端: McpConfigTab] 
     │ (GET /api/agents/:id/mcp-servers)
     ▼
[code-opc Server: workspace_mcp_api] ───(判断是否部门工作区)───> [PlatformClient]
     │                                                               │
     │ (返回动态合并列表: source="enterprise", enabled=true)          │ GET /integration/v1/mcp/services
     ▼                                                               ▼
[前端展示企业工具卡片]                                      [platform-opc Gateway]
                                                                     ▲
                                                                     │ POST /integration/v1/mcp/invoke-task
[Agent CLI (Claude/Codex)] ──(tools/call)──> [remote_mcp_broker] ────┘
```

---

## 3. 组件与接口细节

### 3.1 平台通信客户端（`code-opc/server/pkg/platformclient`）
- **职责**：封装与 `platform-opc` 网关的 HTTP 交互。
- **配置项**：
  - `PLATFORM_GATEWAY_URL`（如 `http://10.101.102.116:3002`）；
  - `PLATFORM_SERVICE_TOKEN`（系统服务调用凭据）；
  - 基础 HTTP Client：超时时间设为 12s。
- **方法**：
  - `ListServices(ctx context.Context, tenant string, includeSchema bool) ([]EnterpriseMcpService, error)`：
    调用 `GET /integration/v1/mcp/services?tenant={tenant}&include_schema=true`。
  - `InvokeTask(ctx context.Context, req TaskInvocationRequest) (*TaskInvocationResponse, error)`：
    调用 `POST /integration/v1/mcp/invoke-task`。
  - `GetInvocation(ctx context.Context, id, subject, tenant, taskID, device string) (*InvocationStatus, error)`：
    轮询结果。

### 3.2 服务端动态聚合（`code-opc/server/internal/handler/workspace_mcp_api.go`）
- **触发路径**：`GET /api/agents/{id}/mcp-servers` 与 `GET /api/workspaces/{id}/mcp-servers`。
- **租户映射判定**：
  - 检查工作区 Slug，若是 `dept-<dept_code>` 格式，或者通过工作区元数据反查部门编码，根据租户规则计算出 `tenant_id`（如 `20000 + deptIntID`）。
- **聚合逻辑**：
  1. 并行或串行查询本地 `agent_mcp_server`（本地自定义配置）和 `PlatformClient.ListServices`（企业配置）；
  2. 针对平台返回的每个服务，转换为虚拟记录：
     - `id`: `"enterprise-" + service_id`；
     - `name`: `service_id`；
     - `transport`: `"remote_bridge"`；
     - `source`: `"enterprise"`；
     - `enabled`: `true`（若本地存在该 Agent 的显式停用 override，则遵循 override）；
     - `tools`: 携带 inputSchema、description、risk 等元数据；
  3. 将本地服务与企业服务合并输出。

### 3.3 任务认领下发（`code-opc/server/internal/handler/daemon.go`）
- 在 `FinalizeTaskClaim` 中，若当前任务执行关联的 Agent 启用了企业 MCP：
  - 组装 `RemoteMCPConnection`：
    - `Name`: `service_id`；
    - `Type`: `"platform_bridge"`；
    - `Endpoint`: `PLATFORM_GATEWAY_URL`；
    - `ServiceToken`: `PLATFORM_SERVICE_TOKEN`；
    - `Context`: `{ task_id, subject: task.UserID, tenant, device: runtime.DeviceID }`；
  - 下发至本地 Daemon。

### 3.4 运行时 Broker 调度代理（`code-opc/server/internal/daemon/remote_mcp_broker.go`）
- 本地 Daemon 监听环回端口 `127.0.0.1:<port>/<token>`。
- 为任务生成并注入 MCP 配置（`mcpServers: { ... }`）。
- **工具调用转换**：
  - 接收 Agent CLI 发起的 `tools/call` JSON-RPC；
  - 向 `platform-opc` 发起 `POST /integration/v1/mcp/invoke-task`；
  - 轮询等待任务状态直至 `completed`（或超时）；
  - 将 `output` 包装回标准 JSON-RPC 结果返回给 Agent。

### 3.5 前端呈现（`code-opc/packages/views/agents/components/tabs/mcp-config-tab.tsx`）
- 对 `source === "enterprise"` 的卡片：
  - 渲染特殊的「企业 (Enterprise)」或「平台提供」标签；
  - 隐藏“编辑配置 / 更改命令”等破坏性按钮，保留“启用 / 停用”开关；
  - 展开可清晰查看包含的工具列表与参数 Schema。

---

## 4. 异常处理与容灾
1. **平台暂时不可用**：
   - 当 `PlatformClient.ListServices` 遇到超时（12s）或 502/503 时，记 Warn 日志，返回当前已有的本地 MCP 列表，不向前端抛出 500 导致整个设置页白屏崩溃。
2. **单个工具调用失败**：
   - 若上游执行失败或审批拒绝，Broker 捕获具体错误（如 `POLICY_DENIED`），以标准 JSON-RPC error（`isError: true`）回传给 Agent CLI，供 LLM 进行自我修正或提示用户，不造成 Broker 崩溃。
3. **短效缓存优化**：
   - 在服务端针对同一 `tenant` 的 `ListServices` 结果实施 30s 内存缓存（带单飞机制 SingleFlight），避免同一工作区多个 Agent 同时加载时对平台产生突发请求。

---

## 5. 安全与权限边界
- **凭据零落地**：上游系统账密、Token、企业微信 Secret 始终存在于 `platform-opc`，绝不下发到 `code-opc` 前端浏览器或用户本机 CLI。
- **任务绑定审计**：所有工具调用严格附带 `task_id`、`subject`、`tenant`、`device`，平台端审计日志完整可追溯。

---

## 6. 测试与验证策略
1. **单测（Go）**：
   - `pkg/platformclient/client_test.go`：覆盖正常拉取、Schema 注入、错误码与超时容错；
   - `internal/handler/workspace_mcp_api_test.go`：测试聚合接口，验证企业服务标记与默认开启；
   - `internal/daemon/remote_mcp_broker_test.go`：测试 JSON-RPC `tools/list` 与 `tools/call` 转换。
2. **联调验证**：
   - 登录桌面客户端，进入部门工作区；
   - 点击 Agent -> 设置 -> MCP 管理，确认默认展示平台已有工具且默认激活；
   - 分配一个要求使用工具的任务，验证 Agent CLI 能顺利调通平台工具并完成任务。
