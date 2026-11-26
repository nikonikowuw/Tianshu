# API Contract

本文件是 Tianshu 后端 HTTP 契约的单一来源。`swag` 注释生成的是派生文档，不能反过来覆盖本文件。新增/修改接口先改这里，再写 handler 与前端消费代码。

## 1. 基础约定

- 业务接口统一前缀 `/api/v1`。
- 探针：`/healthz`（进程存活）、`/readyz`（PostgreSQL 等依赖就绪）。
- 请求/响应体默认 `application/json; charset=utf-8`；文件上传用 `multipart/form-data`，下载单独处理。
- 时间统一 RFC3339 UTC（如 `2026-11-26T00:00:00Z`）；ID 统一 UUID 字符串。
- 布尔值只用 `true`/`false`；禁止 `0/1`、`"yes"`。
- 成功响应无信封：直接返回 DTO，不包 `{ code, message, data }`。
- 前端可忽略未知字段；后端新增字段视为兼容变更。

## 2. Request ID

- 每个 API 响应必须带 `X-Request-Id` 响应头；它是 request id 的唯一权威位置。
- 请求可带 `X-Request-Id`；只接受 `[A-Za-z0-9_-]{8,64}`，否则服务端重新生成。
- 错误体不重复携带 `request_id`；前端从响应头读取，`slog` 使用同一值。
- CORS 配置必须暴露 `X-Request-Id`，否则浏览器读不到；同源/代理部署天然可读。

## 3. 成功响应与状态码

| 状态码 | 场景                                               |
| ------ | -------------------------------------------------- |
| `200`  | 查询、更新、动作成功且有响应体                     |
| `201`  | 创建成功；响应体返回新对象                         |
| `204`  | 删除、登出等无内容操作；不得返回 body              |
| `400`  | JSON 语法错误、Content-Type 不支持、请求体无法解析 |
| `401`  | 未登录、Access Token 无效/过期、Refresh Token 无效 |
| `403`  | 已登录但无权限；账号被禁用                         |
| `404`  | 资源不存在，或路由不存在                           |
| `409`  | 唯一键冲突、状态冲突、Setup 已完成、系统角色不可改 |
| `413`  | 上传体超过限制                                     |
| `422`  | 字段级参数校验失败                                 |
| `429`  | 触发限流；带 `Retry-After`                         |
| `500`  | 未预期内部错误；不暴露 SQL/堆栈                    |
| `503`  | 依赖未就绪/维护模式；`/readyz` 失败                |

- 区分 `400` 与 `422`：解析不了请求体是 `400`；能解析但字段不合法是 `422`。
- 禁止用 `200` 返回业务错误；禁止把 `500` 包装成 `200`。
- `204` 不返回 JSON，前端不要尝试 `response.json()`。

## 4. 分页

请求参数：

| 参数       | 类型 | 默认 | 约束              |
| ---------- | ---- | ---- | ----------------- |
| `page`     | int  | `1`  | `>= 1`            |
| `pageSize` | int  | `20` | `10..100, <=1000` |

响应固定：

```json
{
  "items": [],
  "total": 100,
  "page": 1,
  "pageSize": 20
}
```

- `total` 是满足筛选条件的总行数，不是当前页数量。
- `items` 为空必须是 `[]`，不能是 `null`。
- `page` 超出总页数时返回空 `items`，不返回 `404`；前端负责回退有效页。
- 删除/筛选导致总页数变小时，后端不自动改请求页码；前端重新查询并回退。
- 需要稳定排序的分页必须自带唯一 tie-breaker（如 `created_at DESC, id DESC`）。

## 5. 错误体

固定结构：

```json
{
  "code": "USERS_EMAIL_CONFLICT",
  "message": "邮箱已被使用",
  "errors": [{ "field": "email", "message": "请输入有效的邮箱地址" }]
}
```

- `code`：稳定、可编程判断的大写错误码；带模块前缀（`USERS_`、`ROLES_`、`AUTH_` 等）。
- `message`：已按 `Accept-Language` 翻译的展示文案；客户端优先直接展示。
- `errors`：仅 `422` 出现；普通错误不带该字段。
- `errors[].field` 使用请求 DTO 的 JSON 字段名；嵌套字段用点路径（`profile.email`）。
- `errors` 按 field 稳定排序并去重；同一字段只返回一条。
- 禁止把数据库错误、SQL、堆栈、内部路径放进 `message` 或 `errors`。
- `request_id` 不在错误体中；客户端从 `X-Request-Id` 响应头读取，错误对象里的 `requestId` 由 HTTP 拦截器填充。

错误码基线：

| code                        | HTTP | 说明                           |
| --------------------------- | ---- | ------------------------------ |
| `VALIDATION_FAILED`         | 422  | 字段校验失败                   |
| `AUTH_INVALID_CREDENTIALS`  | 401  | 用户名/邮箱或密码错误          |
| `AUTH_ACCOUNT_DISABLED`     | 403  | 账号被禁用                     |
| `AUTH_FORBIDDEN`            | 403  | 无权限                         |
| `AUTH_TOKEN_EXPIRED`        | 401  | Access Token 过期              |
| `AUTH_REFRESH_REUSED`       | 401  | Refresh Token 重放，会话已吊销 |
| `SETUP_ALREADY_INITIALIZED` | 409  | Setup 已完成                   |
| `USERS_EMAIL_CONFLICT`           | 409  | 邮箱冲突                       |
| `USERS_USERNAME_CONFLICT`        | 409  | 用户名冲突                     |
| `USERS_NOT_FOUND`                | 404  | 用户不存在                     |
| `USERS_INVALID_PASSWORD`         | 422  | 密码长度不满足 8–128 要求      |
| `USERS_INVALID_ROLE`             | 422  | 角色 ID 无效或不存在           |
| `USERS_LAST_SUPER_ADMIN`         | 409  | 最后一名活跃超管保护           |
| `USERS_SELF_OPERATION_FORBIDDEN` | 409  | 禁止对自身执行删除/禁用/摘除超管 |
| `USERS_SUPER_ADMIN_PROTECTED`    | 403  | 非超管禁止操作超管账号或授予/撤销超管角色 |
| `ROLES_SYSTEM_IMMUTABLE`         | 409  | 系统角色不可改/删              |
| `ROLES_CODE_CONFLICT`       | 409  | 角色编码已存在                 |
| `ROLES_IN_USE`              | 409  | 角色仍绑定用户                 |
| `MENUS_NOT_FOUND`           | 404  | 菜单节点不存在                 |
| `MENUS_CODE_CONFLICT`       | 409  | 菜单编码已存在                 |
| `MENUS_SYSTEM_IMMUTABLE`    | 409  | 系统菜单不可删除或修改编码/类型 |
| `MENUS_HAS_CHILDREN`        | 409  | 菜单仍包含子节点               |
| `MENUS_CYCLE_DETECTED`      | 409  | 菜单父级不能指向自身或子孙节点 |
| `MENUS_INVALID_PARENT`      | 422  | 父级菜单不存在或类型不兼容     |
| `FILES_TYPE_NOT_ALLOWED`    | 400  | MIME/类型不允许                |
| `FILES_TOO_LARGE`           | 413  | 文件过大                       |
| `RATE_LIMITED`              | 429  | 限流                           |
| `INTERNAL_ERROR`            | 500  | 未预期错误                     |

错误码一旦发布不得改名；废弃用新码 + 迁移期兼容，不静默改变语义。

## 6. 参数校验

- 使用 `go-playground/validator` 或等价库；`binding` tag 只做结构校验，业务规则放 `service.go`。
- 字段名以 JSON tag 为准，不能把 Go 字段名暴露给前端。
- 校验失败统一 `422 VALIDATION_FAILED`，由 `ErrorHandler` 翻译 `validator` 错误。
- 密码、邮箱、用户名等规则必须在 service 层再校验一次，不能只依赖 handler。
- 未知 JSON 字段默认忽略；需要严格模式时在 DTO 上显式配置，不要全局开启导致兼容性问题。
- 空字符串、`null`、缺省值语义必须明确；不要用零值猜“未传”。

## 7. 查询、筛选与排序

- 常用参数：`q`（关键词）、`sort`（字段）、`order`（`asc`/`desc`）、`status`、`role` 等。
- 已知参数非法返回 `422`；未知参数忽略，保持向前兼容。
- `sort` 只接受端点白名单；禁止把用户输入拼进 `ORDER BY`。
- 默认排序必须稳定，并在端点文档/Swagger 注释中写明。
- 数组参数用重复 key 或逗号分隔必须由 API 明确约定；前端按同一规则序列化。
- 搜索使用参数化 `ILIKE`/全文索引；禁止拼接 SQL。

## 8. 认证与 Cookie

- Access Token：`Authorization: Bearer <token>`；只放前端内存。
- Refresh Token：HttpOnly Cookie，Path `/api/v1/auth`；具体属性、轮换与重放检测见 [security.md](./security.md)。
- 登录/刷新/Setup 接口不要求 Bearer；登出可只带 Refresh Cookie。
- 禁止把 Token 放 query string、图片 URL、日志或错误上报。
- 401 与 403 语义分开：401 是“没登录/凭证无效”，403 是“已登录但没权限/被禁用”。

## 9. 健康检查

`GET /healthz`：

```json
{ "status": "ok" }
```

- 只表示进程存活，不查 DB；必须快速返回。

`GET /readyz`：

```json
{
  "status": "ok",
  "checks": {
    "postgres": "ok"
  }
}
```

- 依赖失败返回 `503`，`checks` 中标出失败项；不要泄露连接串/密码。
- 容器编排只把 `/readyz` 作为 readiness，`/healthz` 作为 liveness。

## 10. 文档与契约测试

- 开发环境可暴露 `/swagger` 或 `/docs`；生产默认关闭。
- 每个端点至少有契约测试：成功、401/403、422、404/409（适用时）、分页、`X-Request-Id`、i18n 文案。
- 前端手写 DTO 与本文对齐；不引入 OpenAPI 双向代码生成。
- 破坏性变更必须新开 `/api/v2`，或在 PRD/本文件明确迁移窗口；新增字段不算破坏性变更。

## 11. Users 与 Role Options 端点契约

### `GET /api/v1/roles/options`

- 权限：`users:read` 或 `roles:read`（满足其一即可）。
- 响应 `200`：`{ "items": [{ "id": "uuid", "code": "super_admin", "name": "超级管理员", "isSystem": true }] }`；系统角色 `name` 按 `Accept-Language` 翻译，排序 `is_system DESC, code ASC`。

### `GET /api/v1/users`（`users:read`）

- 查询参数：
  - `page`（默认 `1`）、`pageSize`（默认 `20`，范围 `10..100`）
  - `q`（最大 100 字符）：匹配 `username`、`email`、`display_name`（大小写不敏感 `ILIKE`）
  - `status`（重复 key：`?status=active&status=disabled`，白名单 `active` / `disabled`）
  - `roleId`（重复 key：`?roleId=<uuid>`，多值取并集匹配）
  - `sort`（白名单：`created_at` / `updated_at` / `username` / `last_login_at`）、`order`（`asc` / `desc`）；默认排序：`created_at DESC, id DESC`
- 响应 `200`：`Paginated<UserDTO>`，`UserDTO` 字段：`id`, `username`, `email`, `displayName`, `status`, `lastLoginAt`（RFC3339 或 `null`）, `roles`（`[{ id, code, name, isSystem }]`，空为 `[]`）, `createdBy` / `updatedBy`（`{ id, username }` 或 `null`）, `createdAt`, `updatedAt`。

### `GET /api/v1/users/:id`（`users:read`）

- 响应 `200`：`UserDTO`；不存在返回 `404 USERS_NOT_FOUND`。

### `POST /api/v1/users`（`users:create`）

- 请求：`{ username, email, displayName?, password, status?, roleIds[] }`（`username` 3–64、`email` ≤255、`displayName` ≤100、`password` 8–128、`status` 默认 `active`）。
- 响应 `201`：`UserDTO`。非 `super_admin` 授予 `super_admin` 返回 `403 USERS_SUPER_ADMIN_PROTECTED`；无效 `roleIds` 返回 `422`；用户名/邮箱冲突返回 `409`。

### `PATCH /api/v1/users/:id`（`users:update`）

- 请求（全可选指针语义，未传表示不修改；`username` 不可修改）：`{ email?, displayName?, status?, password?, roleIds? }`。
- 响应 `200`：`UserDTO`。
- 约束：禁止非超管编辑/禁用/重设超管账号或授予/撤销超管（`403 USERS_SUPER_ADMIN_PROTECTED`）；禁止禁用自己或移除自己的 `super_admin`（`409 USERS_SELF_OPERATION_FORBIDDEN`）；禁用或移除最后一名活跃超管返回 `409 USERS_LAST_SUPER_ADMIN`。禁用或重置密码时吊销目标用户全部活跃会话，角色变更在事务提交后失效目标用户权限缓存。

### `DELETE /api/v1/users/:id`（`users:delete`）

- 响应 `204`（无响应体）。软删除用户，同事务内清空 `user_roles` 并吊销全部活跃会话。
- 约束：禁止删除自身（`409 USERS_SELF_OPERATION_FORBIDDEN`）；禁止非超管删除超管（`403 USERS_SUPER_ADMIN_PROTECTED`）；禁止删除最后一名活跃超管（`409 USERS_LAST_SUPER_ADMIN`）。

## 12. Menus 端点契约

- `GET /api/v1/menus/tree`（`menus:read`）：返回完整菜单树 `{ "items": [MenuTreeNode...] }`（按 `sortOrder ASC, createdAt ASC` 排序）。
- `GET /api/v1/menus/user`（已登录即可）：返回当前用户有权限且 `visible=true`、`status='active'` 的导航菜单树 `{ "items": [MenuTreeNode...] }`（自动裁剪无可见子项的空目录）。
- `POST /api/v1/menus`（`menus:create`）：新增目录/菜单/按钮节点，返回 `201 MenuNodeDTO`。
- `PATCH /api/v1/menus/:id`（`menus:update`）：编辑节点、调整 `parentId` 与 `sortOrder`，返回 `200 MenuNodeDTO`。
- `PUT /api/v1/menus/reorder`（`menus:update`）：批量调整菜单节点父级与排序 `{ "items": [{ "id", "parentId", "sortOrder" }] }`，返回 `200 { "items": [MenuTreeNode...] }`。
- `DELETE /api/v1/menus/:id`（`menus:delete`）：删除自定义叶子菜单节点，返回 `204`。

## 13. 暂不纳入 v1

- 通用 `Idempotency-Key` 重放表：Setup 用数据库咨询锁/唯一约束保证幂等；文件上传用 SHA-256 去重作为后续增强。
- GraphQL、gRPC 对外接口、批量导入接口。
- 多租户/租户级 API 前缀。
