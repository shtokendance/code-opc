# 部门工作区自动供给与权限映射 实施计划

> **针对智能体工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务实施此计划。步骤使用复选框 (`- [ ]`) 语法进行追踪。

**目标：** 实现用户登录时根据 Token 中的部门信息自动分配/创建部门工作区并设置主管/成员角色，同时禁止普通用户自建工作区。

**架构：** 网关在企微登录时通过账户中心/企微提取部门名称、编码与主管身份写入 JWT Claims；Multica 服务端在鉴权中间件与 `GetMe` 端点提取 Claims 进行 JIT 工作区供给与成员角色绑定，并启用 `DISABLE_WORKSPACE_CREATION` 配置限制用户自建。

**技术栈：** Go 1.23, Gin, Chi, pgx/v5, sqlc, JWT (HMAC-SHA256), Docker.

---

## 文件结构规划

### 1. 平台网关 (`/Users/fangqq/Code/tokendance/platform-opc`)
- 修改：`pkg/accountcenter/client.go`：新增 `GetDepartmentByCode` 方法，根据部门编码直接拉取部门名称及主管 ID。
- 修改：`pkg/handler/handler.go`：在 `code-desktop` 与 `code-web:` 登录分支中提取部门详情并注入 Claims（`dept_code`, `dept_name`, `is_dept_manager`）。
- 测试：`pkg/handler/handler_code_jwt_test.go`：验证 Code 客户端登录时签发的 JWT 包含正确的部门字段与主管布尔值。

### 2. Code 服务端 (`/Users/fangqq/Code/tokendance/code-opc`)
- 修改：`server/internal/middleware/auth.go`：在 JWT 解析流程中提取 `dept_code`、`dept_name`、`is_dept_manager` 并通过请求头注入上下文。
- 修改：`server/internal/handler/auth.go`：在 `GetMe` 端点中执行 JIT 用户同步、部门工作区按需创建与主管角色自动升级。
- 测试：`server/internal/handler/auth_dept_workspace_test.go`：覆盖新用户部门工作区创建、已有工作区加入、主管角色升级测试。
- 修改：`docker-compose.yml` / 启动配置：配置 `DISABLE_WORKSPACE_CREATION=true`。

---

### 任务 1：网关注入部门信息与主管标识到 Code JWT (`platform-opc`)

**文件：**
- 修改：`/Users/fangqq/Code/tokendance/platform-opc/pkg/accountcenter/client.go`
- 修改：`/Users/fangqq/Code/tokendance/platform-opc/pkg/handler/handler.go`
- 测试：`/Users/fangqq/Code/tokendance/platform-opc/pkg/handler/handler_code_jwt_test.go`

- [ ] **步骤 1：编写测试用例验证 Code JWT 中包含部门声明与主管标识**

创建 `/Users/fangqq/Code/tokendance/platform-opc/pkg/handler/handler_code_jwt_test.go`：

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestCodeDesktopLoginIncludesDepartmentClaims(t *testing.T) {
	fakeAC := newSyncFakeAccountCenter(t)
	fakeAC.seed() // T01 技术中心, u1 员工 (dept: T01)

	h := &Handler{
		ac: fakeAC.client(),
	}

	claims := h.buildCodeJWTClaims("u1", "u1@corp.test", "员工一", "T01", "技术中心", true)
	if claims["dept_code"] != "T01" {
		t.Fatalf("expected dept_code T01, got %v", claims["dept_code"])
	}
	if claims["dept_name"] != "技术中心" {
		t.Fatalf("expected dept_name 技术中心, got %v", claims["dept_name"])
	}
	if claims["is_dept_manager"] != true {
		t.Fatalf("expected is_dept_manager true, got %v", claims["is_dept_manager"])
	}
}
```

- [ ] **步骤 2：运行测试验证其失败**

运行：`cd /Users/fangqq/Code/tokendance/platform-opc && go test -v ./pkg/handler -run TestCodeDesktopLoginIncludesDepartmentClaims`
预期：失败，提示 `buildCodeJWTClaims` 未定义。

- [ ] **步骤 3：在 client.go 和 handler.go 中实现部门查询与 Claims 注入**

在 `/Users/fangqq/Code/tokendance/platform-opc/pkg/accountcenter/client.go` 新增：
```go
// GetDepartmentByCode 根据部门编码查询单个部门
func (c *Client) GetDepartmentByCode(ctx context.Context, departmentCode string) (*SysDepartment, error) {
	if strings.TrimSpace(departmentCode) == "" {
		return nil, nil
	}
	query := `
		query GetDepartmentByCode($code: String!) {
			sysDepartments(
				filter: { departmentCode: { eq: $code }, isDeleted: { eq: 0 } }
				paging: { first: 1 }
			) {
				edges {
					node {
						id
						parentCode
						departmentCode
						departmentName
						departmentSort
						departmentStatus
						departmentLevel
						thirdDepartmentId
						thirdManagerId
					}
				}
			}
		}
	`
	var resp struct {
		SysDepartments struct {
			Edges []struct {
				Node SysDepartment `json:"node"`
			} `json:"edges"`
		} `json:"sysDepartments"`
	}
	if err := c.query(ctx, query, map[string]interface{}{"code": departmentCode}, &resp); err != nil {
		return nil, err
	}
	if len(resp.SysDepartments.Edges) == 0 {
		return nil, nil
	}
	dept := resp.SysDepartments.Edges[0].Node
	return &dept, nil
}
```

在 `/Users/fangqq/Code/tokendance/platform-opc/pkg/handler/handler.go` 中提取助手函数 `buildCodeJWTClaims` 并更新 `code-desktop` / `code-web:` 分支：
```go
func (h *Handler) buildCodeJWTClaims(userID, email, username, deptCode, deptName string, isDeptManager bool) jwt.MapClaims {
	userUUID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("code-opc:wecom:"+userID)).String()
	sid := uuid.New().String()
	return jwt.MapClaims{
		"sub":             userUUID,
		"email":           email,
		"name":            username,
		"dept_code":       deptCode,
		"dept_name":       deptName,
		"is_dept_manager": isDeptManager,
		"sid":             sid,
		"exp":             time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":             time.Now().Unix(),
	}
}
```
并在 `handler.go` 回调中调用：
```go
	var deptCode, deptName string
	isLeader := false
	if acUser != nil && acUser.DepartmentCode != "" {
		deptCode = acUser.DepartmentCode
		dept, _ := h.ac.GetDepartmentByCode(ctx, deptCode)
		if dept != nil {
			deptName = dept.DepartmentName
			if dept.ThirdManagerID != "" && acUser.ThirdUserID != "" && dept.ThirdManagerID == acUser.ThirdUserID {
				isLeader = true
			}
		}
	}

	claims := h.buildCodeJWTClaims(detail.UserID, email, username, deptCode, deptName, isLeader)
```

- [ ] **步骤 4：运行测试验证其通过**

运行：`cd /Users/fangqq/Code/tokendance/platform-opc && go test -v ./pkg/handler -run TestCodeDesktopLoginIncludesDepartmentClaims`
预期：通过（PASS）。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/platform-opc
git add pkg/accountcenter/client.go pkg/handler/handler.go pkg/handler/handler_code_jwt_test.go
git commit -m "feat(auth): inject department code, name, and manager claims into code jwt"
```

---

### 任务 2：服务端解析声明并 JIT 自动创建/绑定部门工作区 (`code-opc`)

**文件：**
- 修改：`/Users/fangqq/Code/tokendance/code-opc/server/internal/middleware/auth.go`
- 修改：`/Users/fangqq/Code/tokendance/code-opc/server/internal/handler/auth.go`
- 测试：`/Users/fangqq/Code/tokendance/code-opc/server/internal/handler/auth_dept_workspace_test.go`

- [ ] **步骤 1：编写失败的 JIT 部门工作区供给测试**

创建 `/Users/fangqq/Code/tokendance/code-opc/server/internal/handler/auth_dept_workspace_test.go`：

```go
package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"multica/server/internal/testutil"
	"multica/server/pkg/db/generated"
)

func TestGetMe_AutoProvisionsDepartmentWorkspaceAndRole(t *testing.T) {
	// 验证携带 X-User-Dept-Code、X-User-Dept-Name、X-User-Is-Dept-Manager 头调用 GetMe 时，
	// 系统会自动创建该工作区并将用户关联为对应角色，且标记 onboarded_at。
	user := testutil.CreateUser(t)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-User-ID", testutil.UUIDToString(user.ID))
	req.Header.Set("X-User-Email", user.Email)
	req.Header.Set("X-User-Dept-Code", "1250300000000000000")
	req.Header.Set("X-User-Dept-Name", "BDE平台开发部")
	req.Header.Set("X-User-Is-Dept-Manager", "true")

	w := httptest.NewRecorder()
	testHandler.GetMe(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 验证工作区已自动创建
	expectedSlug := "dept-1250300000000000000"
	ws, err := testHandler.Queries.GetWorkspaceBySlug(context.Background(), expectedSlug)
	require.NoError(t, err)
	assert.Equal(t, "BDE平台开发部", ws.Name)

	// 验证用户角色为主管 (admin)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: ws.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "admin", member.Role)
}
```

- [ ] **步骤 2：运行测试验证其失败**

运行：`cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/handler -run TestGetMe_AutoProvisionsDepartmentWorkspaceAndRole`
预期：失败，未自动创建工作区或找不到工作区。

- [ ] **步骤 3：编写最小化实现**

1. 在 `code-opc/server/internal/middleware/auth.go` 中，JWT 校验通过后提取 Claims 并设置请求头：
```go
			if deptCode, ok := claims["dept_code"].(string); ok && deptCode != "" {
				r.Header.Set("X-User-Dept-Code", deptCode)
			}
			if deptName, ok := claims["dept_name"].(string); ok && deptName != "" {
				r.Header.Set("X-User-Dept-Name", deptName)
			}
			if isManager, ok := claims["is_dept_manager"].(bool); ok && isManager {
				r.Header.Set("X-User-Is-Dept-Manager", "true")
			}
```

2. 在 `code-opc/server/internal/handler/auth.go` 中扩展 `GetMe`：
```go
	deptCode := strings.TrimSpace(r.Header.Get("X-User-Dept-Code"))
	deptName := strings.TrimSpace(r.Header.Get("X-User-Dept-Name"))
	isDeptManager := r.Header.Get("X-User-Is-Dept-Manager") == "true"

	if deptCode != "" {
		h.ensureDepartmentWorkspace(r.Context(), user, deptCode, deptName, isDeptManager)
	}
```

实现 `ensureDepartmentWorkspace`：
- Slug 命名规则：`dept-` + 清理后的小写字母数字；
- 检查 `GetWorkspaceBySlug`：若不存在则调用 `CreateWorkspace`，Name 设为 `deptName`，IssuePrefix 设为 `"DP"`；
- 检查 `GetMemberByUserAndWorkspace`：
  - 若不存在，调用 `CreateMember`，角色：主管为 `"admin"`，普通员工为 `"member"`；
  - 若存在且 `isDeptManager` 为 true 而当前角色为 `"member"`，调用 `UpdateMemberRole` 升级为 `"admin"`；
- 若 `user.OnboardedAt` 为 NULL，调用 `UpdateUserOnboarded` 将其置为 `NOW()`。

- [ ] **步骤 4：运行测试验证其通过**

运行：`cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/handler -run TestGetMe_AutoProvisionsDepartmentWorkspaceAndRole`
预期：通过（PASS）。

- [ ] **步骤 5：提交代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add server/internal/middleware/auth.go server/internal/handler/auth.go server/internal/handler/auth_dept_workspace_test.go
git commit -m "feat(auth): auto-provision department workspace and bind role in GetMe"
```

---

### 任务 3：配置并验证禁止普通用户自建工作区 (`code-opc`)

**文件：**
- 修改：`/Users/fangqq/Code/tokendance/code-opc/server/internal/config/config.go`（确认环境变量读取）
- 测试：`/Users/fangqq/Code/tokendance/code-opc/server/internal/handler/workspace_disabled_test.go`

- [ ] **步骤 1：编写测试验证 DISABLE_WORKSPACE_CREATION 下自建被拒**

创建 `/Users/fangqq/Code/tokendance/code-opc/server/internal/handler/workspace_disabled_test.go`：

```go
package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"multica/server/internal/testutil"
)

func TestCreateWorkspace_BlockedWhenCreationDisabled(t *testing.T) {
	testHandler.Config().DisableWorkspaceCreation = true
	defer func() { testHandler.Config().DisableWorkspaceCreation = false }()

	user := testutil.CreateUser(t)
	body := []byte(`{"name":"My Custom WS","slug":"my-custom-ws"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testutil.UUIDToString(user.ID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	testHandler.CreateWorkspace(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}
```

- [ ] **步骤 2：运行测试验证其通过**

运行：`cd /Users/fangqq/Code/tokendance/code-opc/server && go test -v ./internal/handler -run TestCreateWorkspace_BlockedWhenCreationDisabled`
预期：通过（PASS）。

- [ ] **步骤 3：提交测试代码**

```bash
cd /Users/fangqq/Code/tokendance/code-opc
git add server/internal/handler/workspace_disabled_test.go
git commit -m "test(workspace): verify creation is forbidden when disabled by config"
```

---

### 任务 4：部署至测试服务器并进行端到端验证 (`10.101.102.116`)

**目标：**
在 `10.101.102.116` 上更新网关与 Multica 后端，确保新环境变量生效，并完成实操验证。

- [ ] **步骤 1：编译并部署网关 `tokendance-gateway`**

```bash
cd /Users/fangqq/Code/tokendance/platform-opc
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tokendance-gateway main.go
scp -i ~/.ssh/id_temp_weknora tokendance-gateway ecs-user@10.101.102.116:/tmp/tokendance-gateway
ssh -i ~/.ssh/id_temp_weknora ecs-user@10.101.102.116 "sudo docker cp /tmp/tokendance-gateway weknora-gateway:/app/tokendance-gateway && sudo docker restart weknora-gateway"
```

- [ ] **步骤 2：编译并部署后端 `multica-backend`**

```bash
cd /Users/fangqq/Code/tokendance/code-opc/server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o multica-server ./cmd/server
scp -i ~/.ssh/id_temp_weknora multica-server ecs-user@10.101.102.116:/tmp/multica-server
ssh -i ~/.ssh/id_temp_weknora ecs-user@10.101.102.116 "sudo docker cp /tmp/multica-server multica-backend:/app/server && sudo docker restart multica-backend"
```

并在服务器上确认 `DISABLE_WORKSPACE_CREATION=true` 已注入 `multica-backend`。

- [ ] **步骤 3：端到端登录流程验证**

1. 通过测试 Token（包含 `dept_code: "1250300000000000000"`, `dept_name: "BDE平台开发部"`, `is_dept_manager: false`）发起回调验证；
2. 验证数据库中已自动产生 `slug: "dept-1250300000000000000"`，`name: "BDE平台开发部"` 的工作区；
3. 验证当前用户已是该工作区的 `member`；
4. 验证 Web 前端直接跳转到 `/dept-1250300000000000000/issues`，且无任何“新建工作区”按钮。
