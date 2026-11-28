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
| `BAD_REQUEST`               | 400  | 请求格式无效或无法解析         |
| `VALIDATION_FAILED`         | 422  | 字段校验失败                   |
| `AUTH_UNAUTHORIZED`         | 401  | 未登录或认证凭证无效           |
| `AUTH_INVALID_CREDENTIALS`  | 401  | 用户名/邮箱或密码错误          |
| `AUTH_ACCOUNT_DISABLED`     | 403  | 账号被禁用                     |
| `AUTH_FORBIDDEN`            | 403  | 无权限                         |
| `AUTH_ORIGIN_REJECTED`      | 403  | Cookie 认证端点请求来源不在允许范围内 |
| `AUTH_TOKEN_EXPIRED`        | 401  | Access Token 过期              |
| `AUTH_REFRESH_REUSED`       | 401  | Refresh Token 重放，会话已吊销 |
| `RESOURCE_NOT_FOUND`        | 404  | 请求的资源不存在               |
| `RESOURCE_CONFLICT`         | 409  | 资源状态或唯一性冲突           |
| `PAYLOAD_TOO_LARGE`         | 413  | 上传或请求体超过大小限制       |
| `SETUP_ALREADY_INITIALIZED` | 409  | Setup 已完成                   |
| `USERS_EMAIL_CONFLICT`           | 409  | 邮箱冲突                       |
| `USERS_USERNAME_CONFLICT`        | 409  | 用户名冲突                     |
| `USERS_NOT_FOUND`                | 404  | 用户不存在                     |
| `USERS_INVALID_PASSWORD`         | 422  | 密码长度不满足 8–128 要求      |
| `USERS_INVALID_ROLE`             | 422  | 角色 ID 无效或不存在           |
| `USERS_LAST_SUPER_ADMIN`         | 409  | 最后一名活跃超管保护           |
| `USERS_SELF_OPERATION_FORBIDDEN` | 409  | 禁止对自身执行删除/禁用/摘除超管 |
| `USERS_SUPER_ADMIN_PROTECTED`    | 403  | 非超管禁止操作超管账号或授予/撤销超管角色 |
| `PROFILE_INVALID_CURRENT_PASSWORD` | 400 | 改密时当前密码校验失败（使用 400 避免触发前端 401 刷新重放） |
| `PROFILE_PASSWORD_UNCHANGED`     | 422  | 新密码与当前密码相同           |
| `PROFILE_INVALID_AVATAR`         | 422  | 指定的头像文件不存在、非就绪图片或不归属当前用户 |
| `PROFILE_SESSION_NOT_FOUND`      | 404  | 目标活跃会话不存在、已失效或不属于当前用户 |
| `PROFILE_CURRENT_SESSION_REVOKE_FORBIDDEN` | 409 | 禁止经会话下线接口吊销当前设备会话（应走 `/auth/logout`） |
| `ACCESS_ROLE_NOT_FOUND`         | 404  | 角色不存在                     |
| `ROLES_SYSTEM_IMMUTABLE`        | 409  | 系统角色不可改/删              |
| `ROLES_CODE_CONFLICT`       | 409  | 角色编码已存在                 |
| `ROLES_IN_USE`              | 409  | 角色仍绑定用户                 |
| `MENUS_NOT_FOUND`           | 404  | 菜单节点不存在                 |
| `MENUS_CODE_CONFLICT`       | 409  | 菜单编码已存在                 |
| `MENUS_SYSTEM_IMMUTABLE`    | 409  | 系统菜单不可删除或修改编码/类型 |
| `MENUS_HAS_CHILDREN`        | 409  | 菜单仍包含子节点               |
| `MENUS_CYCLE_DETECTED`      | 409  | 菜单父级不能指向自身或子孙节点 |
| `MENUS_INVALID_PARENT`      | 422  | 父级菜单不存在或类型不兼容     |
| `FILES_NOT_FOUND`           | 404  | 文件不存在或未就绪             |
| `FILES_TYPE_NOT_ALLOWED`    | 400  | MIME/类型不允许或与扩展名不匹配 |
| `FILES_TOO_LARGE`           | 413  | 文件过大                       |
| `FILES_EMPTY_FILE`          | 400  | 不允许上传空文件               |
| `FILES_MISSING_FILE`        | 400  | 缺少上传文件字段               |
| `FILES_INVALID_IMAGE`       | 400  | 头像图片损坏或像素尺寸超限     |
| `FILES_IN_USE`              | 409  | 文件正被未删除用户头像引用     |
| `FILES_INVALID_SIGNED_URL`  | 401  | 签名访问链接无效或已过期       |
| `FILES_STORAGE_UNAVAILABLE` | 503  | 对应存储驱动当前未配置/不可用  |
| `RATE_LIMITED`              | 429  | 限流                           |
| `INTERNAL_ERROR`            | 500  | 未预期错误                     |
| `SERVICE_UNAVAILABLE`       | 503  | 服务暂不可用                   |

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

## 11. Roles、Permissions 与 Users 端点契约

### `GET /api/v1/permissions`（`roles:read`）

- 响应 `200`：`{ "groups": [{ "module": "dashboard", "label": "仪表盘", "permissions": [{ "code": "dashboard:read", "module": "dashboard", "name": "查看仪表盘", "description": "..." }] }] }`；分组标题与权限名称/描述按 `Accept-Language` 实时翻译，空数组序列化为 `[]`。

### `GET /api/v1/roles`（`roles:read`）

- 查询参数：
  - `page`（默认 `1`）、`pageSize`（默认 `20`，范围 `10..100`）
  - `q`（最大 100 字符，拒绝 NUL 字节与非法 UTF-8）：大小写不敏感 `ILIKE` 匹配 `code`、`name`、`description`，并匹配系统内置角色的中英译名/译述
  - `isSystem`（严格布尔值 `true` / `false`，非法值返 `422`）
  - `sort`（白名单：`created_at` / `updated_at` / `code` / `name`）、`order`（`asc` / `desc`）；默认排序：`is_system DESC, created_at DESC, id DESC`（按 `name` 排序时系统内置角色按当前语言译名参与排序）
- 响应 `200`：`Paginated<RoleDTO>`，`RoleDTO` 字段：`id`, `code`, `name`, `description`（系统角色按 `Accept-Language` 翻译）, `isSystem`, `userCount`, `permissionCount`, `permissions`（`string[]`，已去重按字典序升序返回，空为 `[]`）, `createdBy` / `updatedBy`（UUID 字符串或 `null`）, `createdAt`, `updatedAt`（RFC3339）。

### `GET /api/v1/roles/:id`（`roles:read`）

- 响应 `200`：`RoleDTO`；不存在返回 `404 ACCESS_ROLE_NOT_FOUND`。

### `POST /api/v1/roles`（`roles:create`）

- 请求：`{ code, name, description?, permissions[] }`（`code` 2–64 位小写字母开头 `[a-z0-9_-]`，`name` 1–100 字符，`description` ≤500 字符，`permissions` 为已注册权限码集合，自动去重排序）。
- 响应 `201`：`RoleDTO`。编码冲突返 `409 ROLES_CODE_CONFLICT`；未知权限码或非法字段返 `422 VALIDATION_FAILED`；同事务写 `role.create` 审计。

### `PATCH /api/v1/roles/:id`（`roles:update`）

- 请求（可选指针语义，`code` 不可修改）：`{ name?, description?, permissions? }`。
- 响应 `200`：`RoleDTO`。系统内置角色禁止修改（`409 ROLES_SYSTEM_IMMUTABLE`）；不存在返 `404 ACCESS_ROLE_NOT_FOUND`；权限变更在最外层事务提交后主动失效关联用户权限缓存，同事务写 `role.update` 审计。

### `DELETE /api/v1/roles/:id`（`roles:delete`）

- 响应 `204`（无响应体）。系统内置角色禁止删除（`409 ROLES_SYSTEM_IMMUTABLE`）；仍绑定用户的角色禁止删除（`409 ROLES_IN_USE`）；不存在返 `404 ACCESS_ROLE_NOT_FOUND`；同事务写 `role.delete` 审计。

### `GET /api/v1/roles/options`

- 权限：`users:read` 或 `roles:read`（满足其一即可）。
- 响应 `200`：`{ "items": [{ "id": "uuid", "code": "super_admin", "name": "超级管理员", "isSystem": true }] }`；系统角色 `name` 按 `Accept-Language` 翻译，排序 `is_system DESC, code ASC`。

### `GET /api/v1/users`（`users:read`）

- 查询参数：
  - `page`（默认 `1`，须为 `>= 1` 的整数）、`pageSize`（默认 `20`，须为 `10..100` 的整数）；非整数或越界返回 `422 VALIDATION_FAILED`
  - `q`（最大 100 字符，拒绝 NUL 字节与非法 UTF-8）：匹配 `username`、`email`、`display_name`（大小写不敏感 `ILIKE`）
  - `status`（重复 key：`?status=active&status=disabled`，白名单 `active` / `disabled`）
  - `roleId`（重复 key：`?roleId=<uuid>`，多值取并集匹配；拒绝 Nil UUID，最多 20 个去重项）
  - `sort`（白名单：`created_at` / `updated_at` / `username` / `last_login_at`，缺省按 `created_at`）、`order`（`asc` / `desc`，缺省 `desc`）；默认排序：`created_at DESC, id DESC`（仅传 `order=asc` 时按 `created_at ASC, id ASC`）
- 响应 `200`：`Paginated<UserDTO>`，`UserDTO` 字段：`id`, `username`, `email`, `displayName`, `status`, `lastLoginAt`（RFC3339 或 `null`）, `roles`（`[{ id, code, name, isSystem }]`，空为 `[]`，按 `is_system DESC, code ASC` 排序）, `createdBy` / `updatedBy`（`{ id, username }` 或 `null`）, `createdAt`, `updatedAt`。

### `GET /api/v1/users/:id`（`users:read`）

- 响应 `200`：`UserDTO`；不存在返回 `404 USERS_NOT_FOUND`。

### `POST /api/v1/users`（`users:create`）

- 请求：`{ username, email, displayName?, password, status?, roleIds[] }`（`username` 3–64、`email` ≤255、`displayName` ≤100 且不含 NUL、`password` 8–128、`status` 默认 `active`）。
- 响应 `201`：`UserDTO`（`roles` 按 `is_system DESC, code ASC` 返回）。事务内校验操作者未软删除、处于 `active` 且当前会话有效（分别返回 `401` / `403 AUTH_ACCOUNT_DISABLED`）；授予 `super_admin` 受 `usersSuperAdminGuardKey` 串行化校验（非超管返回 `403 USERS_SUPER_ADMIN_PROTECTED`）；无效 `roleIds` 返回 `422`；用户名/邮箱冲突返回 `409`。

### `PATCH /api/v1/users/:id`（`users:update`）

- 请求（全可选指针语义，未传表示不修改；`username` 不可修改）：`{ email?, displayName?, status?, password?, roleIds? }`。
- 响应 `200`：`UserDTO`（`roles` 按 `is_system DESC, code ASC` 返回）。
- 约束：事务内校验操作者未软删除、处于 `active` 且当前会话有效（分别返回 `401` / `403 AUTH_ACCOUNT_DISABLED`，阻止禁用管理员自我解禁）；禁止非超管编辑/禁用/重设超管账号或授予/撤销超管（`403 USERS_SUPER_ADMIN_PROTECTED`）；禁止禁用自己或移除自己的 `super_admin`（`409 USERS_SELF_OPERATION_FORBIDDEN`）；禁用或移除最后一名活跃超管返回 `409 USERS_LAST_SUPER_ADMIN`。禁用或重置密码时吊销目标用户全部活跃会话，角色集合实际变更时在事务提交后失效目标用户权限缓存。

### `DELETE /api/v1/users/:id`（`users:delete`）

- 响应 `204`（无响应体）。软删除用户，同事务内清空 `user_roles` 并吊销全部活跃会话。
- 约束：事务内校验操作者未软删除、处于 `active` 且当前会话有效（分别返回 `401` / `403 AUTH_ACCOUNT_DISABLED`）；禁止删除自身（`409 USERS_SELF_OPERATION_FORBIDDEN`）；禁止非超管删除超管（`403 USERS_SUPER_ADMIN_PROTECTED`）；禁止删除最后一名活跃超管（`409 USERS_LAST_SUPER_ADMIN`）。

## 12. Menus 端点契约

- `GET /api/v1/menus/tree`（`menus:read` 或 `roles:read`）：返回完整菜单树 `{ "items": [MenuNodeDTO...] }`（按 `sortOrder ASC, createdAt ASC, id ASC` 排序）。
- `GET /api/v1/menus/user`（已登录即可）：返回当前用户有权限且 `visible=true`、`status='active'` 的导航菜单树 `{ "items": [MenuNodeDTO...] }`（自动裁剪无可见子项的空目录与 `button` 节点）。
- `POST /api/v1/menus`（`menus:create`）：新增目录/菜单/按钮节点，返回 `201 MenuNodeDTO`。
- `PATCH /api/v1/menus/:id`（`menus:update`）：编辑节点、调整 `parentId` 与 `sortOrder`，返回 `200 MenuNodeDTO`。系统菜单不可修改 `code`/`type`（`409 MENUS_SYSTEM_IMMUTABLE`），自定义菜单不可修改 `code`（`422 code: ACCESS_MENU_CODE_IMMUTABLE`）。
- `PUT /api/v1/menus/reorder`（`menus:update`）：批量调整菜单节点父级与排序 `{ "items": [{ "id", "parentId", "sortOrder" }] }`（1–500 项），返回 `200 { "items": [MenuNodeDTO...] }`。
- `DELETE /api/v1/menus/:id`（`menus:delete`）：删除自定义叶子菜单节点，返回 `204`。
- `MenuNodeDTO` 字段：`id`、`parentId`（UUID 或 `null`）、`type`（`directory` / `menu` / `button`）、`code`、`title`（系统菜单优先按 `Accept-Language` 翻译）、`path`、`icon`、`permissionCode`、`sortOrder`、`visible`、`status`（`active` / `disabled`）、`isSystem`、`children`（`MenuNodeDTO[]`，空为 `[]`）、`createdBy` / `updatedBy`（UUID 或 `null`）、`createdAt` / `updatedAt`（RFC3339）。
- `access` 模块字段级校验键（通过 `422 VALIDATION_FAILED` 的 `errors[].message` 翻译返回）：`ACCESS_INVALID_ROLE_CODE`、`ACCESS_INVALID_ROLE_NAME`、`ACCESS_INVALID_ROLE_DESCRIPTION`、`ACCESS_INVALID_PERMISSION_CODE`、`ACCESS_INVALID_NUMBER`、`ACCESS_INVALID_BOOL`、`ACCESS_INVALID_MENU_CODE`、`ACCESS_MENU_CODE_IMMUTABLE`、`ACCESS_INVALID_MENU_TYPE`、`ACCESS_INVALID_MENU_TITLE`、`ACCESS_INVALID_MENU_PATH`、`ACCESS_INVALID_MENU_ICON`、`ACCESS_INVALID_MENU_STATUS`、`ACCESS_DUPLICATE_MENU_ID`、`MENUS_INVALID_PARENT`。

## 13. Files 端点契约

- `GET /api/v1/files/config`（已登录即可）：返回上传策略 `{ "maxFileSizeBytes", "maxAvatarSizeBytes", "allowedExtensions" }`，供前端文件选择器与大小提示使用；前端不得硬编码后端可配置的上限与类型。配置扩展 `storage.allowed_mime_types` 仅接受可映射安全扩展名的类型，忽略含危险/可执行扩展名或无法安全映射的类型。
- `POST /api/v1/files`（`files:upload` + 限流 20 次/分钟/用户）：`multipart/form-data` 字段 `file`；执行有界读取（含 `MaxBytesError` 与 `multipart.ErrMessageTooLarge` 统一返 `413 FILES_TOO_LARGE`）、文件名清洗后内容嗅探 + 扩展名交叉校验（显式拒绝 `image/svg+xml` 与 HTML/JS）、SHA-256 计算、`pending`→`ready` 两阶段持久化与 `file.upload` 审计；返回 `201 FileDTO`。
- `POST /api/v1/files/avatar`（已登录且账号活跃、会话未吊销 + 限流 20 次/分钟/用户）：`multipart/form-data` 字段 `file`；上限默认 2 MiB，仅允许安全位图格式并经纯 Go 缩放至 ≤512px 重编码为 PNG 剥离全部 EXIF 元数据，写 `file.upload_avatar` 审计；返回 `201 FileDTO`。
- `GET /api/v1/files`（`files:read`）：查询 `status = 'ready'` 分页列表，支持 `page`、`pageSize`、`q`（文件名 `ILIKE`，合法 UTF-8 且不含 NUL、≤100 字符）、`category`（`image` / `document`）、`mimeType`（≤20 项，每项为合法 UTF-8、不含 NUL 且 ≤128 字符）、`driver`（`local` / `s3`）、`sort`（白名单 `created_at` / `original_name` / `size_bytes`）、`order`（`asc` / `desc`）；多值筛选兼容重复键与逗号分隔；默认排序 `created_at DESC, id DESC`；参数非法返回 `422 VALIDATION_FAILED`（字段级错误码含 `FILES_INVALID_PAGE`、`FILES_INVALID_PAGE_SIZE`、`FILES_QUERY_TOO_LONG`、`FILES_INVALID_CATEGORY`、`FILES_INVALID_DRIVER`、`FILES_TOO_MANY_MIME_FILTERS`、`VALIDATION_ONE_OF`、`VALIDATION_DEFAULT`）；返回 `200 Paginated<FileDTO>`。
- `GET /api/v1/files/:id`（`files:read`）：返回 `200 FileDTO`，不存在返回 `404 FILES_NOT_FOUND`。
- `GET /api/v1/files/:id/content`（`files:read`，可选 `?download=1` 或 `?download=true`）：按记录的 `storage_driver` 路由驱动流式输出；内置安全位图默认 `inline`（带下载参数为 `attachment`），非内置安全位图强制 `Content-Disposition: attachment`（`filename*` 按 RFC 8187 编码），始终附带 `X-Content-Type-Options: nosniff`、`Content-Security-Policy: default-src 'none'; sandbox` 与 `Cache-Control: private, no-store`。
- `POST /api/v1/files/:id/signed-url`（`files:read`，可选 `{ "disposition": "inline" | "attachment" }`，非法值返 `422` `disposition: FILES_INVALID_DISPOSITION`）：签发短时 HMAC-SHA256 访问链接 `{ "url": "/api/v1/files/signed/<token>", "expiresAt": "..." }`（默认 5 分钟；非内置安全位图强制 `attachment`）。
- `GET /api/v1/files/signed/:token`（公开验签路由，免 Bearer）：校验 HMAC 签名与过期时间后流式输出，使用与 `/content` 相同的安全响应头策略；无效或过期返回 `401 FILES_INVALID_SIGNED_URL`。
- `DELETE /api/v1/files/:id`（`files:delete`）：事务内校验无未删除用户头像引用（命中返回 `409 FILES_IN_USE`）后标为 `deleting` 并写 `file.delete` 审计；事务提交后删除存储对象再物理删行，存储失败由后台 Janitor 经宽限期后重试清理；返回 `204`。

`FileDTO` 字段：`id`、`originalName`、`mimeType`、`category`（`image` / `document`）、`sizeBytes`、`sha256`、`storageDriver`（`local` / `s3`）、`isImage`（与 `category === 'image'` 口径一致）、`uploader`（`{ id, username }` 或 `null`）、`createdAt` / `updatedAt`（RFC3339）。`SignedFileURLDTO` 字段：`url`、`expiresAt`（RFC3339）。`UploadPolicyDTO` 字段：`maxFileSizeBytes`、`maxAvatarSizeBytes`、`allowedExtensions`（含点号、升序、无重复）。

## 14. Profile & Sessions 端点契约

以下端点挂载于 `/api/v1/profile`（`identity` 模块），仅要求已登录（Bearer JWT），不要求额外管理员权限码；同时 `POST /api/v1/auth/login` 与 `GET /api/v1/auth/me` 中的 `user` 对象新增兼容字段 `avatarFileId`（UUID 或 `null`）与 `avatarUrl`（短时签名路径或 `null`）。

`GET /api/v1/auth/me` 除 Bearer JWT 验签外，还要求令牌 `sid` 对应的会话仍处于未吊销且未过期状态；会话失效返回 `401 AUTH_UNAUTHORIZED`，用于前端“被踢下线后不再恢复登录态”。

- `GET /api/v1/profile`：返回 `200 ProfileDTO`（`id`, `username`, `email`, `displayName`, `avatarFileId`, `avatarUrl`, `status`, `lastLoginAt`, `roles`（系统角色名按 `Accept-Language` 翻译）, `createdAt`, `updatedAt`）。
- `PATCH /api/v1/profile`：请求 `{ displayName?, email?, avatarFileId? }`（均可选；`avatarFileId` 区分缺省不改、`null`/空串清空、UUID 绑定）；绑定头像在事务内对 `files` 行加 `FOR SHARE` 锁并校验 `status='ready'`、安全图片 MIME、归属当前用户且位于 `avatars/` 前缀下（失败返 `422` `avatarFileId: PROFILE_INVALID_AVATAR`）；邮箱唯一冲突返 `409 USERS_EMAIL_CONFLICT`；写 `profile.update` 审计；返回 `200 ProfileDTO`。
- `PUT /api/v1/profile/password`（限流 5 次/分钟/用户，超限 `429 RATE_LIMITED` + `Retry-After`）：请求 `{ currentPassword, newPassword, revokeOtherSessions? }`（`revokeOtherSessions` 默认 `true`）；旧密码错误写失败审计并返 `400 PROFILE_INVALID_CURRENT_PASSWORD`；新旧相同返 `422` `newPassword: PROFILE_PASSWORD_UNCHANGED`；成功后更新 Argon2id 哈希、按开关吊销除当前会话外的活跃会话并写 `profile.change_password` 审计，返回 `200 { "revokedSessions": N }`。
- `GET /api/v1/profile/sessions`：返回当前用户全部未吊销且未过期的活跃会话 `{ "items": [{ "id", "ipAddress", "userAgent", "isCurrent", "lastUsedAt", "createdAt", "expiresAt" }] }`（当前会话置顶，绝不返回令牌哈希）。
- `DELETE /api/v1/profile/sessions/:id`：吊销指定活跃会话（仅限本人）；目标为当前会话返 `409 PROFILE_CURRENT_SESSION_REVOKE_FORBIDDEN`；不存在/已失效/非本人返 `404 PROFILE_SESSION_NOT_FOUND`；写 `profile.session_revoke` 审计；返回 `204`。
- `DELETE /api/v1/profile/sessions`：一键吊销除当前会话外的所有其他活跃会话，实际吊销 `N > 0` 条时写 `profile.sessions_revoke_others` 审计，返回 `200 { "revokedSessions": N }`。

## 15. Dashboard 端点契约

- `GET /api/v1/dashboard/summary`（`dashboard:read`）：只读聚合系统核心统计与近期动态，支持可选查询参数 `days`（默认 `7`，白名单 `7` / `14` / `30`；非法值返 `422 VALIDATION_FAILED` + 字段错误 `days: DASHBOARD_INVALID_DAYS`）。
- 响应 `200`（裸 DTO，无信封）：
  - `generatedAt`（RFC3339 UTC）、`rangeDays`（`7` / `14` / `30`）；
  - `users`：`{ total, active, disabled, newLast7Days }`（均排除软删除用户）；
  - `roles`：`{ total, system, custom }`；
  - `files`：`{ total, totalSizeBytes, images, documents }`（仅统计 `status = 'ready'`，按 `image/%` 区分图片与文档）；
  - `sessions`：`{ active }`（未吊销、未过期且所属用户未删除且为 `active`）；
  - `audit`：`{ totalInRange, loginsInRange, failuresInRange }`；
  - `activityTrend`：按 UTC 日期升序连续补零的 `rangeDays` 项 `[{ date, logins, operations, failures }]`（`logins` 为成功登录、`operations` 为非登录成功操作、`failures` 为失败事件，三者互斥无重复计数）；
  - `recentActivity`：最新 8 条脱敏审计摘要 `[{ id, actorUsername, action, resource, resourceId, status, ipAddress, createdAt }]`（不暴露 `metadata` 与 `userAgent`，空切片序列化为 `[]`）。

## 16. Audit Logs 端点契约

审计日志为**严格只读**接口，不提供任何 `POST`/`PUT`/`PATCH`/`DELETE` 写端点；历史记录仅由服务端后台任务按 `audit.retention_days` 分批清理。

- `GET /api/v1/audit-logs`（`audit:read`）：返回 `200 Paginated<AuditLogDTO>`（无信封，空 `items` 序列化为 `[]`）。
  - 查询参数：
    - `page`（可选整数，空串回落默认 `1`，必须 `>= 1`；非整数返 `422` `AUDIT_INVALID_NUMBER`，超界返 `AUDIT_INVALID_PAGE`）
    - `pageSize`（可选整数，空串回落默认 `20`，范围 `10..100`；非整数返 `422` `AUDIT_INVALID_NUMBER`，超界返 `AUDIT_INVALID_PAGE_SIZE`）
    - `q`（可选，最大 100 个 Unicode 字符，超限返 `422` `AUDIT_QUERY_TOO_LONG`）：大小写不敏感转义 `ILIKE` 匹配 `actor_username`、`action`、`resource`、`resource_id`、`request_id`、`ip_address`
    - `actor`（可选，最大 100 个 Unicode 字符，超限返 `422` `AUDIT_ACTOR_TOO_LONG`）：大小写不敏感转义 `ILIKE` 匹配 `actor_username`
    - `actorId`（可选非零 UUID；格式非法或 Nil UUID `00000000-0000-0000-0000-000000000000` 均返 `422` `VALIDATION_UUID`）
    - `action`、`resource`（可选多值数组，支持重复 key 如 `?action=a&action=b` 或逗号分隔如 `?action=a,b`，自动 trim 与去重，单值 ≤64 字符且最多 20 项；超限返 `422` `AUDIT_INVALID_ACTION` / `AUDIT_INVALID_RESOURCE`）
    - `status`（可选多值数组，同上规则，白名单 `success` / `failure`；非法值返 `422` `AUDIT_INVALID_STATUS`）
    - `from`、`to`（可选 RFC3339 / RFC3339Nano 时间戳，统一转 UTC 比较，闭区间 `[from, to]`；格式非法返 `422` `AUDIT_INVALID_TIME_FORMAT`，`from > to` 返 `422` `from: AUDIT_INVALID_TIME_RANGE`）
    - `sort`（白名单 `created_at` / `action` / `resource` / `status`，默认 `created_at`；非法值返 `422` `VALIDATION_ONE_OF`）
    - `order`（白名单 `asc` / `desc`，默认 `desc`；非法值返 `422` `VALIDATION_ONE_OF`）
    - 默认排序：`created_at DESC, id DESC`（带确定性 UUIDv7 tie-breaker）。
  - `AuditLogDTO` 字段：`id`（UUIDv7）、`actorId`（UUID 或 `null`）、`actorUsername`、`action`、`resource`、`resourceId`、`status`（`success` / `failure`）、`ipAddress`、`userAgent`（按 UTF-8 字符边界截断 ≤512 字节）、`requestId`、`metadata`（对象，落库前已深度脱敏，空时为 `{}`）、`createdAt`（RFC3339 UTC）。
  - 模块级字段校验键（均通过 `422 VALIDATION_FAILED` 的 `errors[].message` 按 `Accept-Language` 翻译）：`AUDIT_INVALID_STATUS`、`AUDIT_INVALID_ACTION`、`AUDIT_INVALID_RESOURCE`、`AUDIT_INVALID_TIME_FORMAT`、`AUDIT_INVALID_TIME_RANGE`、`AUDIT_INVALID_NUMBER`、`AUDIT_INVALID_PAGE`、`AUDIT_INVALID_PAGE_SIZE`、`AUDIT_QUERY_TOO_LONG`、`AUDIT_ACTOR_TOO_LONG`。

## 17. 暂不纳入 v1

- 通用 `Idempotency-Key` 重放表：Setup 用数据库咨询锁/唯一约束保证幂等；文件上传用 SHA-256 去重作为后续增强。
- GraphQL、gRPC 对外接口、批量导入接口。
- 多租户/租户级 API 前缀。
