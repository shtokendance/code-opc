# 部门工作区自动供给与权限映射规范设计

## 1. 目标与背景
实现企业微信/统一账户中心登录与 Multica 工作区和角色的无缝映射：
1. **默认部门工作区**：用户登录时默认采用其所属部门作为工作区，若该部门工作区不存在则自动创建，存在则直接加入。
2. **禁止普通用户创建工作区**：关闭普通用户自建工作区权限，所有工作区完全由企业组织架构驱动。
3. **部门主管自动管理员**：若用户为部门主管，自动成为该部门工作区的管理员（`admin`）；普通员工为普通成员（`member`）。当主管发生变动时，下次登录时自动同步更新角色。
4. **零破坏性**：本改动仅作用于 `code-desktop` 与 `code-web:` 客户端登录流，对 `knowledge-opc`（WeKnora）0 侵入，不影响任何其他平台业务。

## 2. 架构设计

### 2.1 网关层（`platform-opc`）
- **触发路径**：`GET /wecom/callback`，当 `state` 为 `code-desktop` 或以 `code-web:` 为前缀时。
- **部门与主管信息解析**：
  - 从统一账户中心 `acUser` 提取 `DepartmentCode` 与 `ThirdUserID`；
  - 查询对应部门实体（`SysDepartment`），获得 `DepartmentName` 与主管 `ThirdManagerID`；
  - 判断是否为主管：`isLeader = (targetDept != nil && targetDept.ThirdManagerID == acUser.ThirdUserID)`，或回退至企微原生 `is_leader`；
  - 构造确定性 `sub`（用户 UUID，基于邮箱或企微 unionNo 保持一致）。
- **JWT Claims 扩展**：
  在签发给 Code 客户端的 JWT 中增加以下声明：
  - `dept_code`: 部门唯一编码（如 `"1250300000000000000"`）
  - `dept_name`: 部门名称（如 `"BDE平台开发部"`）
  - `is_dept_manager`: 是否为主管布尔值（`true`/`false`）

### 2.2 服务端层（`code-opc/server`）
- **中间件鉴权**：
  - 在 `middleware.Auth` 中解析 JWT Claims，将 `dept_code`、`dept_name`、`is_dept_manager` 写入 Context 或 Request Header（`X-User-Dept-Code`, `X-User-Dept-Name`, `X-User-Is-Dept-Manager`）。
- **`GetMe` JIT 自动拨备**：
  - **用户同步**：确保当前用户在 `user` 表中存在（若存在同名邮箱则复用，同时确保当前 token 关联的 ID 能映射）。
  - **部门工作区检查/创建**：
    - 若 `dept_code` 非空，计算工作区 Slug：`dept-<dept_code>`（全小写字母数字和连字符，符合正则）；
    - 查询工作区是否已存在；若不存在，以事务自动创建，工作区名称为 `dept_name`（如“BDE平台开发部”），自动生成合法 `issue_prefix`。
  - **成员关系与角色绑定**：
    - 预期角色：`targetRole = "member"`，若 `is_dept_manager == true` 则 `targetRole = "admin"`；
    - 查询用户是否为该工作区成员：
      - 若非成员：插入一条 `member` 记录；
      - 若已是成员：若用户是主管但当前仅为 `member`，更新角色为 `admin`。
  - **标记引导完成**：
    - 设置 `user.onboarded_at = NOW()`（若为 NULL），确保用户登录后跳过新工作区向导，直接进入部门工作区。
- **全局禁止工作区自建**：
  - 启动配置增加 `DISABLE_WORKSPACE_CREATION=true`；
  - `POST /api/workspaces` 针对普通用户请求校验返回 403 Forbidden；
  - `/api/config` 返回 `workspace_creation_disabled: true`。

### 2.3 前端层（`code-opc/apps/web` & `apps/desktop`）
- 登录成功后，前端拉取工作区列表，由于 `GetMe` 已自动完成拨备并返回了部门工作区，前端路由解析器 `resolvePostAuthDestination` 会将页面直接重定向至 `/{dept-slug}/issues`。
- “新建工作区”按钮因 `workspace_creation_disabled: true` 自动隐藏。

## 3. 错误处理与降级策略
- **未分配部门员工**：若员工在账户中心或企微中无部门，`dept_code` 为空，跳过自动创建，提示联系管理员或进入兜底默认空间。
- **并发创建冲突**：多名员工在同一秒首次登录时，工作区创建事务采用 `ON CONFLICT (slug) DO NOTHING` 并在冲突时重新加载已有工作区。
- **网络波动与账户中心不可达**：网关记录日志并降级至企微原生接口拉取部门详情，保证用户基本登录流程不中断。
