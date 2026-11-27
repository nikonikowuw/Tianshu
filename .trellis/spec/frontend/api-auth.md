# Frontend API, Auth & Permissions

本规范约束前端如何消费后端 HTTP 接口、管理认证会话、执行权限控制与访问受保护文件。

服务端协议（响应结构、状态码、错误码、Cookie 属性、Setup 模式与文件限制）以 [backend/api-contract.md](../backend/api-contract.md) 和 [backend/security.md](../backend/security.md) 为唯一事实源，本文只规定前端接线行为，不复写后端定义。

## 1. HTTP 客户端边界

- 所有业务请求必须经由 `src/lib/api/` 暴露的统一客户端发起，禁止在组件或 feature 中直接调用原生 `fetch` 或新建独立 `axios` 实例。
- 客户端职责收口：
  - 统一 `baseURL`（默认 `/api/v1`）与 `withCredentials: true`（承载后端 HttpOnly Refresh Cookie）；
  - 默认超时 15s，文件上传单独 60s；
  - 自动附带 `Accept-Language`（与当前 i18n 语言同步）；
  - 对需要鉴权的接口附加 `Authorization: Bearer <accessToken>`；公开认证与初始化端点（`/setup/status`、`/setup/init`、`/auth/login`、`/auth/refresh`）不附加；
  - `204 No Content` 响应不解析 JSON。
- 前端 DTO 在 `features/<domain>/types.ts` 与 `lib/api/types.ts` 手写维护，不引入 OpenAPI 双向代码生成；对服务端新增未知字段保持宽容忽略。

## 2. 错误归一化与展示

统一在客户端拦截器中将失败响应归一化为 `ApiError`：

```ts
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: string;
  readonly fieldErrors: Record<string, string>;
  readonly retryAfter: number;
}
```

规则：

- `requestId` 从 `X-Request-Id` 响应头读取（唯一来源），缺失时置空字符串并记开发日志，不伪造。
- `retryAfter` 从 `429` 响应的 `Retry-After` 头读取（秒数向上取整，无效或缺失时为 `0`）；跨域场景需由服务端 `Access-Control-Expose-Headers` 暴露。
- `message` 后端已按 `Accept-Language` 翻译，前端优先直接展示，仅在网络异常或缺失时按 `code` 回退本地文案。
- `422` 的 `errors[]` 映射为 `fieldErrors`，表单提交统一通过 `form.setError` 回填到字段，不只弹全局 toast。
- `5xx` 展示通用本地化错误文案 + `requestId`，禁止暴露堆栈或原始响应体。
- 查询错误在全局 `QueryCache.onError` 兜底（列表/区块内联错误态可标记跳过全局 toast）；mutation 错误在各自 `onError` 处理，严禁同一错误重复弹两次 toast。
- `403` 全局副作用（清会话、跳登录）只对受保护端点生效；公开认证端点（`/auth/login`、`/auth/refresh`、`/auth/logout`、`/setup/*`）的 `401`/`403` 交由页面内联提示，避免同一次失败同时出现 toast、路由跳转与表单横幅。

## 3. TanStack Query 约定

- Query Key 按业务域分层：`[domain, 'list', params]`、`[domain, 'detail', id]`；所有参与请求的参数必须进入 key。
- 返回内容依赖后端本地化翻译的接口（如权限目录、含内置角色名的列表），将当前 `language` 纳入 query key，并在语言切换时失效。
- 列表翻页与筛选使用 `placeholderData: keepPreviousData`，避免视图闪空。
- `queryFn` 必须透传 `signal` 支持请求取消。
- Mutation 成功后按最小范围 `invalidateQueries`；仅在登出或会话失效时执行 `queryClient.clear()`。
- Setup 状态查询配置 `retry: false`、`refetchOnWindowFocus: false`，初始化完成后主动 invalidate。

## 4. 认证会话与单飞刷新

状态机固定为 `unknown | authenticated | unauthenticated`，仅存内存：

1. **启动恢复（`ensureSession`）**：调用 `POST /auth/refresh` 获取 Access Token → 调用 `GET /auth/me` 加载用户、角色与权限码；失败置为 `unauthenticated`。
2. **登录 / Setup 初始化**：Setup 页为免令牌极简向导，不再渲染 `setupToken` 输入框；是否可用仅由 `GET /setup/status` 的 `initialized` 判定，初始化完成后更新并 invalidate Setup 状态并引导至登录页；登录提交凭据后将响应返回的用户、角色、权限码与 Access Token 写入内存会话（`ensureSession` 启动恢复时再拉取 `/auth/me`），并跳转合法 `redirect` 或 `/`。
3. **单飞刷新（Single-Flight Refresh）**：
   - `ensureSession()` 与 `401` 拦截刷新共用同一把单飞锁：并发触发时只发一次 `POST /auth/refresh`，其余请求复用同一个 Promise；
   - 刷新成功后重放原请求一次；刷新请求本身及其重放失败不再触发二次刷新，防止死循环；
   - 刷新失败统一清空内存会话、清空 Query Cache 并跳转 `/sign-in?redirect=...`，不保留半登录态。
4. **登出与跨标签同步**：
   - 调用 `POST /auth/logout`（即使后端失败也继续）→ 清空内存会话与 Query Cache → 跳转 `/sign-in`；
   - 通过浏览器同源通信通道（如 `BroadcastChannel`）向同源其他标签页广播登出事件；跨标签并发刷新依赖后端宽限期，前端不做额外状态猜测。
5. **401 与 403 分流**：
   - `401`（非登录/刷新端点）：走单飞刷新，失败则清会话跳登录；
   - `403 AUTH_ACCOUNT_DISABLED`：账号已被禁用，直接清空会话与 Query Cache 并跳转 `/sign-in` 提示原因；
   - `403 AUTH_FORBIDDEN`：不登出；在非 `/auth/me` 请求上允许触发一次 `/auth/me` 刷新以同步刚变更的权限（防止 `/auth/me` 自身 403 死循环），仍无权限再展示无权限提示。

## 5. 前端权限边界

- 权限码以后端 `/auth/me` 返回的 `permissions: string[]` 为唯一事实源；前端禁止硬编码 `super_admin` 特权绕过。
- 权限校验通过统一抽象（`hasPermission` / `usePermission` / `<PermissionGuard>`）暴露，便于在测试中替换权限上下文。
- 导航菜单、命令面板与页面操作按钮复用同一套权限码声明；导航分组下所有子项无权限时隐藏整个分组。
- 危险操作（删除、禁用、重置密码等）：无权限时隐藏；有权限但当前业务状态不允许时禁用并附 tooltip 说明。
- 前端权限只服务 UX，后端始终是唯一鉴权边界。

## 6. 受保护文件与头像访问

因 Access Token 仅放内存且仅通过 `Authorization` 头传递，`<img src>` 与直接 `<a href>` 无法携带凭证：

- 优先调用后端签名 URL 接口获取短时访问地址用于图片渲染与下载；
- 若仅提供 Bearer 鉴权的 `/content` 端点，前端必须通过带凭证的请求获取 `Blob` 并用 `URL.createObjectURL` 渲染，组件卸载或 URL 替换时必须调用 `URL.revokeObjectURL` 释放内存；
- 禁止将 Access Token 拼接进 URL query string；
- 头像加载失败回退至姓名首字母占位；上传前可做大小与 MIME 预检，但以后端校验为准。
