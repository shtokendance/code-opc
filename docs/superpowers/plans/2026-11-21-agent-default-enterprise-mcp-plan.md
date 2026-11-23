# 智能体默认获取企业已拥有 MCP 实施计划

> **针对智能体工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务实施此计划。步骤使用复选框 (`- [ ]`) 语法进行追踪。

**目标：** 实现智能体进入部门工作区后默认拉取并启用企业平台（platform-opc / WeKnora）已授权的 MCP 工具，并在 Agent 执行任务时通过本地 Broker 安全转发调度。

**架构：** 在 `code-opc/server` 引入 `pkg/platformclient` 负责与 `platform-opc` 网关通信。在 `workspace_mcp_api.go` 的查询接口中动态将平台企业服务与本地 MCP 合并返回给前端，标记 `source="enterprise"` 且默认 `enabled=true`。在任务认领时将企业 MCP 注入 `RemoteMCPConnection`，由 Daemon 的 `remote_mcp_broker` 将 `tools/call` 转译为 `POST /integration/v1/mcp/invoke-task` 执行。

**技术栈：** Go (Chi, Net/HTTP, JSON-RPC), TypeScript, React, TanStack Query, Zod.

---

### 文件结构映射

- **新建文件**：
  - `server/pkg/platformclient/models.go`：企业平台 MCP 服务的结构体定义。
  - `server/pkg/platformclient/client.go`：与 `platform-opc` HTTP 交互的客户端封装。
  - `server/pkg/platformclient/client_test.go`：客户端单元测试。
- **修改文件**：
  - `packages/core/types/workspace.ts`：扩展 `WorkspaceMcpServer` 接口类型（`source`, `description`, `tools`）。
  - `packages/core/api/schemas.ts`：扩展 `WorkspaceMcpServerSchema` Zod 校验。
  - `server/internal/handler/workspace_mcp_api.go`：支持动态聚合企业 MCP 服务。
  - `server/internal/handler/workspace_mcp_api_test.go`：服务端聚合逻辑单元测试。
  - `server/pkg/remotemcp/types.go`：增加 `EnterpriseContributionPrefix = "enterprise:"`。
  - `server/internal/daemon/remote_mcp_broker.go`：支持将企业工具调用转发至 `invoke-task`。
  - `packages/views/agents/components/tabs/mcp-config-tab.tsx`：前端渲染企业标签与只读保护。

---

### 任务 1：创建 `platformclient` 模块并实现企业服务拉取

**文件：**
- 创建：`server/pkg/platformclient/models.go`
- 创建：`server/pkg/platformclient/client.go`
- 测试：`server/pkg/platformclient/client_test.go`

- [ ] **步骤 1：编写失败的客户端测试**

在 `server/pkg/platformclient/client_test.go` 中编写针对 `ListServices` 的单元测试：

```go
package platformclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListServices_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/integration/v1/mcp/services" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("tenant") != "20001" {
			t.Errorf("unexpected tenant: %s", r.URL.Query().Get("tenant"))
		}
		if r.URL.Query().Get("include_schema") != "true" {
			t.Errorf("expected include_schema=true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"services": [
				{
					"service_id": "weknora-kb",
					"tools": [
						{
							"name": "kb_search",
							"description": "搜索企业知识库",
							"input_schema": {"type": "object", "properties": {"query": {"type": "string"}}}
						}
					]
				}
			]
		}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	services, err := client.ListServices(context.Background(), "20001", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].ServiceID != "weknora-kb" {
		t.Errorf("expected service_id=weknora-kb, got %s", services[0].ServiceID)
	}
	if len(services[0].Tools) != 1 || services[0].Tools[0].Name != "kb_search" {
		t.Errorf("expected tool kb_search, got %+v", services[0].Tools)
	}
}

func TestListServices_TimeoutOrError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": "upstream unavailable"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 1*time.Second)
	_, err := client.ListServices(context.Background(), "20001", true)
	if err == nil {
		t.Fatal("expected error on 502, got nil")
	}
}
```

- [ ] **步骤 2：运行测试并验证其失败**

运行：
```bash
cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./pkg/platformclient
```
预期：编译失败，因为 `Client` 和 `NewClient` 尚未定义。

- [ ] **步骤 3：编写 `models.go` 与 `client.go`**

创建 `server/pkg/platformclient/models.go`：
```go
package platformclient

import "encoding/json"

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Risk        string          `json:"risk,omitempty"`
}

type EnterpriseMcpService struct {
	ServiceID string           `json:"service_id"`
	Tools     []ToolDefinition `json:"tools"`
}

type ListServicesResponse struct {
	Services []EnterpriseMcpService `json:"services"`
}
```

创建 `server/pkg/platformclient/client.go`：
```go
package platformclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(baseURL, serviceToken string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		serviceToken: strings.TrimSpace(serviceToken),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) ListServices(ctx context.Context, tenant string, includeSchema bool) ([]EnterpriseMcpService, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	endpoint := fmt.Sprintf("%s/integration/v1/mcp/services", c.baseURL)
	q := url.Values{}
	if tenant != "" {
		q.Set("tenant", tenant)
	}
	if includeSchema {
		q.Set("include_schema", "true")
	}
	fullURL := endpoint + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list services request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute list services: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list services failed with status %d: %s", resp.StatusCode, string(body))
	}

	var res ListServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode list services response: %w", err)
	}
	return res.Services, nil
}
```

- [ ] **步骤 4：运行测试并验证其通过**

运行：
```bash
cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./pkg/platformclient
```
预期：`PASS`。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add server/pkg/platformclient
git commit -m "feat(platformclient): implement platform mcp client for listing enterprise services"
```

---

### 任务 2：扩展核心类型定义以支持企业 MCP 标识与工具描述

**文件：**
- 修改：`packages/core/types/workspace.ts`
- 修改：`packages/core/api/schemas.ts`
- 修改：`server/internal/handler/workspace_mcp_api.go:24-35`

- [ ] **步骤 1：在 `packages/core/types/workspace.ts` 中扩展 `WorkspaceMcpServer` 接口**

```typescript
export interface WorkspaceMcpToolSummary {
  name: string;
  description?: string;
  input_schema?: unknown;
}

export interface WorkspaceMcpServer {
  id: string;
  workspace_id: string;
  name: string;
  transport: string;
  enabled?: boolean;
  agent_count?: number;
  source?: "local" | "enterprise" | "plugin";
  description?: string;
  tools?: WorkspaceMcpToolSummary[];
  created_at: string;
  updated_at: string;
}
```

- [ ] **步骤 2：在 `packages/core/api/schemas.ts` 中更新 Zod 校验 Schema**

在 `WorkspaceMcpServerSchema` 中加入：
```typescript
export const WorkspaceMcpToolSummarySchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  input_schema: z.unknown().optional(),
});

export const WorkspaceMcpServerSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  name: z.string().default(""),
  transport: z.string().default("unknown"),
  enabled: z.boolean().optional(),
  agent_count: z.number().int().nonnegative().optional().catch(undefined),
  source: z.enum(["local", "enterprise", "plugin"]).optional(),
  description: z.string().optional(),
  tools: z.array(WorkspaceMcpToolSummarySchema).optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});
```

- [ ] **步骤 3：在 `server/internal/handler/workspace_mcp_api.go` 中更新 Go 响应模型**

```go
type WorkspaceMcpToolSummary struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type WorkspaceMcpServerResponse struct {
	ID          string                    `json:"id"`
	WorkspaceID string                    `json:"workspace_id"`
	Name        string                    `json:"name"`
	Transport   string                    `json:"transport"`
	Enabled     *bool                     `json:"enabled,omitempty"`
	AgentCount  *int64                    `json:"agent_count,omitempty"`
	Source      string                    `json:"source,omitempty"`
	Description string                    `json:"description,omitempty"`
	Tools       []WorkspaceMcpToolSummary `json:"tools,omitempty"`
	CreatedAt   string                    `json:"created_at"`
	UpdatedAt   string                    `json:"updated_at"`
}
```

- [ ] **步骤 4：运行类型检查**

```bash
cd /Users/fangqq/Code/tokendance/code-opc && pnpm typecheck
```
预期：类型检查全部通过。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add packages/core/types/workspace.ts packages/core/api/schemas.ts server/internal/handler/workspace_mcp_api.go
git commit -m "feat(types): expand WorkspaceMcpServer with source, description, and tools"
```

---

### 任务 3：在服务端聚合接口中动态合入部门企业 MCP 服务

**文件：**
- 修改：`server/internal/handler/workspace_mcp_api.go`
- 测试：`server/internal/handler/workspace_mcp_api_test.go`

- [ ] **步骤 1：编写聚合逻辑的单元测试**

在 `server/internal/handler/workspace_mcp_api_test.go` 中增加测试：

```go
func TestListAgentMcpServers_IncludesEnterpriseServicesForDeptWorkspace(t *testing.T) {
	// 验证对于 slug 为 dept- 开头的工作区，接口能够自动合并平台返回的 source="enterprise" 且 enabled=true 的项
}
```

- [ ] **步骤 2：运行测试并观察现有行为**

运行：
```bash
cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/handler -run TestListAgentMcpServers
```

- [ ] **步骤 3：在 `workspace_mcp_api.go` 中实现动态聚合**

在 `ListAgentMcpServers` 与 `ListWorkspaceMcpServers` 中增加平台企业 MCP 的查询与合并逻辑：
1. 从 `agent.WorkspaceID` 读取 `workspace` 行；
2. 如果 `workspace.Slug` 以 `dept-` 开头（或存在 `dept_code` 元数据）：
   - 解析部门 ID 并计算租户 `tenantID = resolveTenantFromDeptCode(deptCode)`；
   - 调用 `h.PlatformClient.ListServices(r.Context(), tenantID, true)`；
   - 将每个服务转换为 `WorkspaceMcpServerResponse`：
     - `ID`: `"enterprise-" + s.ServiceID`；
     - `Name`: `s.ServiceID`；
     - `Source`: `"enterprise"`；
     - `Transport`: `"remote_bridge"`；
     - `Enabled`: `&trueValue`；
     - `Tools`: 转换后的工具清单；
   - 将结果合并至 `resp`。
3. 容错保护：若平台请求出错，使用 `slog.Warn` 记录日志，仅返回本地列表，绝不向客户端抛出 500。

- [ ] **步骤 4：运行测试验证通过**

```bash
cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/handler -run TestListAgentMcpServers
```
预期：`PASS`。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add server/internal/handler/workspace_mcp_api.go server/internal/handler/workspace_mcp_api_test.go
git commit -m "feat(mcp): dynamically synthesize enterprise mcp services for department workspaces"
```

---

### 任务 4：在本地 Daemon Broker 中支持企业 MCP 的任务转发执行

**文件：**
- 修改：`server/pkg/remotemcp/types.go`
- 修改：`server/internal/daemon/remote_mcp_broker.go`
- 测试：`server/internal/daemon/remote_mcp_broker_test.go`

- [ ] **步骤 1：增加企业前缀常量**

在 `server/pkg/remotemcp/types.go` 中：
```go
const (
	PluginContributionPrefix     = "plugin:"
	EnterpriseContributionPrefix = "enterprise:"
)
```

- [ ] **步骤 2：在 `remote_mcp_broker.go` 中添加针对企业工具的调用转发**

当 Agent 发起 `tools/call` 且该工具属于企业 MCP 时：
```go
if strings.HasPrefix(conn.ContributionID, remotemcp.EnterpriseContributionPrefix) {
    // 组装 POST /integration/v1/mcp/invoke-task 请求发往 platform-opc
    // 轮询等待任务结果并组装 JSON-RPC 响应
}
```

- [ ] **步骤 3：编写单测验证 Broker 转发逻辑**

在 `remote_mcp_broker_test.go` 中增加一个 Mock Server 测试企业工具调用的转译。

- [ ] **步骤 4：运行测试**

```bash
cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/daemon -run TestRemoteMCPBroker
```
预期：`PASS`。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add server/pkg/remotemcp/types.go server/internal/daemon/remote_mcp_broker.go server/internal/daemon/remote_mcp_broker_test.go
git commit -m "feat(broker): add platform task invocation support for enterprise mcp tools"
```

---

### 任务 5：前端 `McpConfigTab` 渲染企业标识与只读保护

**文件：**
- 修改：`packages/views/agents/components/tabs/mcp-config-tab.tsx`
- 修改：`packages/views/agents/components/tabs/mcp-config-model.ts`
- 测试：`packages/views/agents/components/tabs/mcp-config-tab.test.tsx`

- [ ] **步骤 1：编写前端组件测试**

验证当列表包含 `source === "enterprise"` 的项时：
1. 卡片上正确渲染「企业」或「平台」徽章；
2. 隐藏编辑配置命令按钮，仅保留启用/停用开关。

- [ ] **步骤 2：更新 `mcp-config-tab.tsx`**

在渲染卡片处：
```tsx
{server.source === "enterprise" && (
  <Badge variant="secondary" className="text-xs">
    企业
  </Badge>
)}
```
对于企业项，禁止弹出自定义命令行编辑弹窗。

- [ ] **步骤 3：运行前端单元测试与构建**

```bash
cd /Users/fangqq/Code/tokendance/code-opc && pnpm test packages/views/agents/components/tabs/mcp-config-tab.test.tsx
```
预期：`PASS`。

- [ ] **步骤 4：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add packages/views/agents/components/tabs/
git commit -m "feat(ui): display enterprise badge and read-only protections in mcp-config-tab"
```

---

### 任务 6：系统联调与端到端实测验证

- [ ] **步骤 1：本地编译打包并验证**
  - 执行 `pnpm build` 与 `make test`。
- [ ] **步骤 2：客户端运行验证**
  - 登录桌面客户端进入部门工作区；
  - 打开 Agent -> 设置 -> MCP 管理，确认默认展示平台已有工具且默认激活；
  - 执行一次任务，验证工具调用端到端打通。
