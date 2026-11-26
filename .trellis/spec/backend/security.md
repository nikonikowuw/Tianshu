# Backend Security

安全基线是发布门禁，不是“后续增强”。任何认证、会话、上传、代理相关改动，都要同时更新本文件与测试。

## 1. 密码

- 使用 `argon2id`，禁止 MD5/SHA1/bcrypt 新用法。
- 基线参数：`memory=64 MiB`、`iterations=3`、`parallelism=2`、`salt=16 bytes`、`key=32 bytes`；写入 PHC 字符串（`$argon2id$v=19$m=65536,t=3,p=2$...`）。
- 参数可配置，但低于基线必须评审；登录成功时若参数已升级，用新参数 rehash 并更新。
- 密码长度 12–128 字符；允许空格与所有 Unicode；不强制大小写/符号组合。
- 可选：接入 breached-password 列表；MVP 不做强制。
- 禁止日志、审计 metadata、错误信息中出现明文密码或哈希。
- 不存在的用户也要执行一次 dummy Argon2 校验，降低用户名枚举的时序差异。

## 2. Access Token

- JWT，`HS256`，密钥至少 32 字节随机值；生产环境缺失/使用默认值必须启动失败。
- Claims：`sub`（user id）、`sid`（session id）、`iat`、`exp`、`iss=tianshu`、`aud=tianshu-web`。
- 有效期默认 15 分钟；允许 30 秒 clock skew；解析时固定算法，禁止接受 `none` 或客户端指定算法。
- **不把 roles/permissions 写进 JWT**。权限每次从缓存/数据库读取，角色变更才能立即生效。
- Access Token 只放前端内存；禁止 localStorage、sessionStorage、JS 可读 Cookie。
- 登出/禁用/删除用户后，Access Token 最多还有 15 分钟残余有效期；文档与测试必须明确这个窗口。

## 3. Refresh Token 与 Session

- 32 字节 `crypto/rand`，base64url 编码；数据库只存 SHA-256 哈希，禁止存明文。
- Cookie：`HttpOnly; Secure(生产); SameSite=Lax; Path=/api/v1/auth; Max-Age=7d`。
- 有效期默认 7 天；每次刷新轮换：旧 token 作废，新 token 写入，`last_used_at` 更新。
- **重放检测（独立历史表）**：采用独立表 `refresh_token_history`（归属 `auth` 模块，至少包含 `token_hash`、`session_id`、`replaced_at`、`expires_at`），轮换时将旧 token 哈希写入历史表而非覆盖丢失，以识别任意代际已作废 token 的重放。
- 检测到超出并发宽限期的已作废 token 重放：立即吊销整个 `session_id`（含关联历史记录），写安全审计事件，返回 `401 AUTH_REFRESH_REUSED`。
- 并发刷新：同一 session 用行锁/`pg_advisory_xact_lock` 串行化；在 `replaced_at + 30~60s` 幂等宽限期内识别多标签页并发，避免互相误杀。
- 登出：吊销当前 session 并清 Cookie；清 Cookie 的 Path/属性必须与设置时一致。
- 改密：校验旧密码，吊销除当前 session 外的所有会话；可选择轮换当前 session。
- 禁用/软删除用户：显式吊销该用户全部 session（软删除不触发外键 `ON DELETE CASCADE`）；若存在物理清理，再由外键级联清理 `user_sessions`。
- 会话列表只返回当前用户自己的记录；`id`、IP、UA、最近使用时间、当前设备标记；不返回 token/hash。
- 过期/已吊销 session 与过期 `refresh_token_history` 由 janitor 定期清理；不要无限增长。

## 4. 登录、Setup 与登出

- 登录支持用户名或邮箱；两者都做大小写不敏感归一化。
- 失败统一返回 `AUTH_INVALID_CREDENTIALS`，不区分“用户不存在/密码错误”。
- 禁用账号返回 `403 AUTH_ACCOUNT_DISABLED`；该提示会暴露账号状态，是后台系统的明确取舍。
- 登录成功更新 `last_login_at`，写成功/失败审计（失败时 `actor_id` 可空，记录尝试的用户名，不记录密码）。
- Setup 仅未初始化时可用；并发提交必须由数据库咨询锁 + `users == 0` 检查保证只有一次成功。
- **Setup 接管防护**：默认 `setup.mode=token`，未初始化时启动生成一次性高熵 Token 输出至 stdout 日志（支持环境变量 `TIANSHU_SETUP_TOKEN` 预设），由向导提交校验；可选 `setup.mode=local`（仅放行 loopback/私网 CIDR）；`setup.mode=open` 必须双保险显式开启（同时配置 `setup.allow_open=true`）并输出 `WARN` 日志；所有模式均受限流与审计约束。
- Setup 成功后 `/setup/init` 永久拒绝；前端 `/setup` 重定向。
- 登出即使后端调用失败，前端也必须清本地会话；后端失败只记日志/提示，不阻止用户离开。
- 不提供注册、社交登录、OTP、忘记密码，除非 PRD 明确加入。

## 5. CSRF、CORS 与 Cookie

- Refresh/Logout 使用 Cookie，必须校验 `Origin`（或 `Referer`）在白名单内；SameSite=Lax 不能替代 Origin 校验。
- CORS 使用精确 origin 白名单 + `Access-Control-Allow-Credentials: true`；禁止 `*` 与 credentials 组合。
- 暴露 `X-Request-Id`；允许 `Authorization`、`Content-Type`、`Accept-Language`、`X-Request-Id` 请求头。
- 生产前后端同源（Go embed），通常不需要 CORS；开发环境通过 Vite proxy 也可避开跨域。CORS 只作为显式配置能力保留。
- Cookie 不使用 `__Host-` 前缀时，Path 必须固定 `/api/v1/auth`；不要给 Cookie 设置过宽的 Domain。
- 禁止把 Refresh Token 放在响应体、URL 或前端可读存储。

## 6. 限流与暴力破解

基线（可配置，按部署规模调整）：

| 端点                 | 限制                             |
| -------------------- | -------------------------------- |
| `POST /auth/login`   | 5 次/分钟/IP + 10 次/分钟/用户名 |
| `POST /auth/refresh` | 30 次/分钟/IP                    |
| `POST /setup/init`   | 5 次/分钟/IP                     |
| 文件上传             | 20 次/分钟/用户                  |
| 全局 API             | 按 IP/用户令牌桶，具体阈值配置化 |

- 超限返回 `429 RATE_LIMITED` + `Retry-After`。
- 限流键要兼容反向代理：只信任配置的 `trusted_proxies`，从可信 `X-Forwarded-For` 取客户端 IP。
- **账号锁定与软冷却**：MVP 不做永久账号锁定，不因登录失败改写 `users.status = 'disabled'`（防止攻击者针对已知用户名锁死管理员）；在双维度限流之上，通过 `platform/cache` 叠加短时自动过期软冷却（同一 `(IP, 账号)` 或单账号连续失败达到阈值，默认 10 次，触发 15 分钟自动过期冷却，返回 `429 RATE_LIMITED` + `Retry-After`）。

## 7. 文件上传与下载

- 大小上限：通用文件默认 **10 MiB**（可配置），头像上传默认 **2 MiB**；Nginx/Go body limit 与应用限制保持一致。
- 先按内容嗅探（前 512 字节）判断 MIME，再校验扩展名与白名单，不能只信客户端 `Content-Type`：
  - 允许图片：`image/jpeg`、`image/png`、`image/webp`、`image/gif`（**显式禁止 `image/svg+xml`**，防止内联脚本 XSS）；
  - 允许文档：`application/pdf`、`text/plain`、`text/csv`、`docx`/`xlsx`/`pptx`（均强制 `Content-Disposition: attachment` 下载，不内联渲染）；
  - 其余类型默认拒绝，支持通过配置扩展白名单。
- 文件名只用于展示；存储 key 用 UUID，禁止路径拼接和 `..`。
- 图片 re-encode：头像上传使用纯 Go 图片库执行 re-encode（缩放至合理尺寸上限如 512×512 并剥离 EXIF/GPS 元数据）；通用文件模块图片 MVP 不做强制 re-encode，依靠白名单、`nosniff`、禁用 SVG 内联与签名 URL 防护。
- 响应头 `X-Content-Type-Options: nosniff`；不要对用户上传内容执行脚本。
- 文件内容访问优先短时签名 URL；若走 Bearer `/content`，前端用 fetch+Blob，禁止把 Token 拼进 URL。
- 上传采用 pending/ready 状态或补偿清理，避免存储成功、DB 失败留下孤儿对象；删除时同样处理双写失败。
- 被头像引用的文件不能硬删；先置空 `avatar_file_id` 或拒绝删除。
- 病毒扫描、图片压缩是可选增强，不作为 MVP 阻塞项。

## 8. 审计与日志

- 审计记录登录成功/失败、登出、Setup、用户/角色写操作、改密、文件上传/删除等关键事件。
- 字段至少：`actor_id`、`actor_username`、`action`、`resource`、`resource_id`、`status`、`ip_address`、`user_agent`、`request_id`、`metadata`。
- `actor_id` 不要用 `ON DELETE CASCADE`；用 `SET NULL` 或干脆不加 FK，保留 `actor_username` 快照。
- 审计日志只读；不提供修改/删除 API。
- metadata 禁止写入密码、token、Cookie、密钥、完整请求体；敏感字段统一 redact。
- 日志使用 `slog` 结构化字段；不打印 Authorization、Cookie、密码、Refresh Token。
- 审计/日志保留策略：默认保留 **180 天**（配置项 `audit.retention_days`，`0` 表示不自动清理），MVP 由 janitor 按批次删除（如每批 5000 行，避免长事务锁表）；MVP 不做表分区，当单表接近千万级或有按月归档诉求时再迁至按 `created_at` 月范围分区。

## 9. 配置、代理与传输

- 生产禁止默认 JWT Secret、默认 DB 密码、默认 MinIO/Redis 凭据；启动校验失败即退出。
- 密钥只从环境变量/密钥管理注入；`config.local.toml`、`.env` 不入库。
- 生产启用 TLS；Go 前面有 Nginx/负载均衡时，配置 `trusted_proxies` 与 `X-Forwarded-Proto`，Cookie `Secure` 才能正确工作。
- 数据库/Redis/MinIO 不暴露公网；生产 compose 只暴露 app/Nginx 端口。
- 安全响应头：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy`、`Permissions-Policy`、CSP、HSTS（TLS 层）。
- CSP 不允许 `unsafe-eval`；如主题脚本必须内联，使用 nonce/hash。
- 依赖安全：CI 跑 `govulncheck`、`golangci-lint`、`pnpm audit`；升级依赖单独提交。
