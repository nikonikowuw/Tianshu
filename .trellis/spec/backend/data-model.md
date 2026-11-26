# Data Model

本文件约束 PostgreSQL 表、约束、索引、迁移与数据生命周期。表结构变更必须同时更新本文件、迁移文件与 `sqlc` 查询。

## 1. 通用约定

- 表名、列名、索引名统一 `snake_case`；表名用复数（`users`、`audit_logs`）。
- 主键统一应用层生成 **UUIDv7**，PostgreSQL 类型 `uuid`；禁止依赖数据库自增。
- 时间统一 `timestamptz`，存 UTC；API 输出 RFC3339。
- 金额/大小用 `bigint` 存最小单位（字节/分），禁止浮点。
- 枚举优先用 `text + CHECK`，不引入 PostgreSQL enum（迁移更简单）。
- 命名：唯一索引 `uq_<table>_<cols>`、普通索引 `idx_<table>_<cols>`、检查 `ck_<table>_<rule>`、外键 `fk_<table>_<ref>`。

## 2. 标准审计字段

核心业务表默认包含：

| 字段         | 类型                                                | 规则                       |
| ------------ | --------------------------------------------------- | -------------------------- |
| `created_at` | `timestamptz not null default now()`                | 创建时间                   |
| `updated_at` | `timestamptz not null default now()`                | 必须由应用更新语句显式写入 |
| `created_by` | `uuid null references users(id) on delete set null` | 系统初始化行可为空         |
| `updated_by` | `uuid null references users(id) on delete set null` | 系统任务可空               |

- 不要只依赖 `default now()` 更新 `updated_at`；`UPDATE` 必须写 `updated_at = now()`，或统一用触发器。首版建议应用层显式写，sqlc 更直观。
- 关联表、会话表、审计表可以豁免四字段，但要在本文件列明例外。
- 审计/日志表不再加 `updated_at`，它们不可变。

## 3. 表所有权

| 模块        | 拥有表                                                   | 说明                                       |
| ----------- | -------------------------------------------------------- | ------------------------------------------ |
| `identity`  | `users`                                                  | 合并原 `user` + `profile`；用户资料、状态、头像引用 |
| `access`    | `roles`、`permissions`、`user_roles`、`role_permissions` | 合并原 `rbac` + `role`；角色/权限/关联     |
| `auth`      | `user_sessions`、`refresh_token_history`                 | 登录会话、Refresh Token 与轮换历史（重放检测） |
| `audit`     | `audit_logs`                                             | 只写/只读审计                              |
| `files`     | `files`                                                  | 文件元数据与存储对象                       |
| `dashboard` | 无                                                       | 只读聚合，不拥有写表                       |
| `setup`     | 无                                                       | 编排 identity/access/auth，不建自己的表    |

规则：

- 写操作只能由拥有表的模块 `Service` 完成；跨模块通过 `deps.go` 接口 + `TxManager.WithinTx` 组合。
- 跨模块读聚合（Dashboard）只读，不更新其他模块的表；复杂聚合用只读查询/视图，并明确归属。
- `setup`/`dashboard` 是编排/读模型模块，可以没有 `store.go`/`dbgen`；这是对固定模块模板的显式例外。

## 4. 核心表

### users

- 列：`id`、`username`、`email`、`password_hash`、`display_name`、`avatar_file_id`、`status`、`last_login_at`、`deleted_at`、审计四字段。
- `username`/`email` 入库前 trim + lowercase；唯一性用部分函数唯一索引（软删除后释放，允许同名/同邮箱重建，新账号分配新 UUIDv7 与历史审计隔离）：
  - `uq_users_username_lower` on `lower(username) WHERE deleted_at IS NULL`
  - `uq_users_email_lower` on `lower(email) WHERE deleted_at IS NULL`
- `status`：`active` / `disabled`，`CHECK` 约束；不要用布尔 `is_active`。
- `avatar_file_id`：`uuid null references files(id) on delete set null`。
- `password_hash` 只出现在认证内部；禁止进入任何 DTO、审计 metadata、日志。
- 删除策略：采用**软删除**（写入 `deleted_at timestamptz null`），所有常规查询默认排除 `deleted_at IS NOT NULL`，并在同一业务流程中显式吊销其全部 `user_sessions`（软删除不触发外键 `ON DELETE CASCADE`）；若未来引入恢复已删除用户能力，需先校验用户名/邮箱唯一冲突。

### roles

- 列：`id`、`code`、`name`、`description`、`name_key`、`description_key`、`is_system`、审计四字段。
- `code` 唯一、稳定、不可改；系统角色 `super_admin` 的 `is_system = true`。
- 系统角色禁止删除、禁止改 code、禁止剥夺全量权限。
- 自定义角色仍绑定用户时禁止删除（`ROLES_IN_USE`）；删除检查与用户分配需要同一事务/锁。
- 双语策略：系统内置角色（`is_system = true`）使用 `name_key`/`description_key`，运行时按 `Accept-Language` 从模块语言包解析展示文案；自定义角色（`is_system = false`）`name_key`/`description_key` 为空，保持用户输入的原始 `name`/`description`。

### permissions

- 列：`code`（PK）、`module`、`name_key`、`description_key`、`updated_at`。
- 权限码在 Go 代码声明，启动时 upsert；DB 是投影，不是声明源。
- 动作粒度约定：权限码采用 `<resource>:<action>` 细粒度动作拆分（`read` / `create` / `update` / `delete`，文件上传为 `upload`），不将增删改合并为单一 `write`。
- 双语策略：存储 `name_key`/`description_key`，运行时按 `Accept-Language` 从模块语言包解析展示文案，不存单语展示文本；`module` 存稳定模块码。
- 删除代码中的权限码时，不要直接级联删除角色关联；先标记 `deprecated`，确认无引用后清理并写审计。
- `role_permissions.permission_code` 外键指向本表；删除权限码前必须处理关联。

### user_roles

- `PK(user_id, role_id)`；`user_id`/`role_id` 均 `ON DELETE CASCADE`。
- 额外索引 `idx_user_roles_role_id`，支持按角色反查用户。
- 分配/移除角色必须走 access 模块 Service，并失效用户权限缓存。

### role_permissions

- `PK(role_id, permission_code)`；均 `ON DELETE CASCADE`。
- 额外索引 `idx_role_permissions_permission_code`。
- 修改角色权限后失效该角色下所有用户的权限缓存；缓存失效必须在事务提交后执行。

### user_sessions

- 列：`id`、`user_id`、`refresh_token_hash`、`user_agent`、`ip_address`、`expires_at`、`revoked_at`、`last_used_at`、`created_at`。
- `user_id` `ON DELETE CASCADE`；`refresh_token_hash` 唯一。
- 索引：`idx_user_sessions_user_id`、`idx_user_sessions_expires_at`、`idx_user_sessions_revoked_at`；清理查询按 `expires_at < now() - interval` 或 `revoked_at` 扫描。
- 只存 token 哈希；绝不存明文。轮换时的历史 token 哈希写入 `refresh_token_history`（见 [security.md](./security.md)）。
- 会话列表只查当前用户；`id` 可暴露，token/hash 不可。

### refresh_token_history

- 列：`token_hash`（PK）、`session_id`（`uuid not null references user_sessions(id) on delete cascade`）、`replaced_at`（`timestamptz not null default now()`）、`expires_at`（`timestamptz not null`）。
- 归属 `auth` 模块，豁免标准审计四字段。
- 索引：`idx_refresh_token_history_session_id`、`idx_refresh_token_history_expires_at`。
- 用于识别任意代际已作废 Refresh Token 的重放，并基于 `replaced_at` 判定 30–60 秒并发刷新宽限期；过期记录随 janitor 定期清理。

### audit_logs

- 列：`id`、`actor_id`、`actor_username`、`action`、`resource`、`resource_id`、`status`、`ip_address`、`user_agent`、`request_id`、`metadata jsonb not null default '{}'`、`created_at`。
- `actor_id` 可空；不要 `ON DELETE CASCADE`。建议 `ON DELETE SET NULL` 或干脆无 FK，保留 `actor_username` 快照。
- `resource_id` 用 `text`，兼容 UUID/外部 key。
- 索引：`idx_audit_logs_created_at`（DESC）、`idx_audit_logs_actor_id_created_at`、`idx_audit_logs_action_created_at`、`idx_audit_logs_resource`、`idx_audit_logs_request_id`。
- 只允许 INSERT/SELECT；不提供 UPDATE/DELETE API。清理按保留策略走 janitor，不走业务接口。
- metadata 禁止写入密码、token、Cookie、密钥；写入前 redact。

### files

- 列：`id`、`uploader_id`、`storage_driver`、`object_key`、`original_name`、`mime_type`、`size_bytes`、`sha256`、`status`、`created_at`、`updated_at`。
- `uploader_id` `ON DELETE SET NULL`；`object_key` 唯一；`sha256` 可加索引用于去重。
- `status`：`pending` / `ready` / `failed`；上传先落 pending，存储成功后置 ready，失败/超时由 janitor 清理。
- `avatar_file_id` 引用本表；删除文件前检查用户头像引用，拒绝或先置空。
- 索引：`idx_files_uploader_id_created_at`、`idx_files_sha256`、`idx_files_status`。
- `storage_driver` 必须参与读路径；不能只按全局配置选驱动，否则切换 local/s3 后旧文件读不到。

## 5. 删除与引用策略

| 关系                              | 策略                              |
| --------------------------------- | --------------------------------- |
| `user_roles` / `role_permissions` | `CASCADE`                         |
| `user_sessions` / `refresh_token_history` | `CASCADE`                         |
| `created_by` / `updated_by`       | `SET NULL`                        |
| `audit_logs.actor_id`             | `SET NULL` 或无 FK，禁止 CASCADE  |
| `users.avatar_file_id`            | `SET NULL`                        |
| 角色被用户引用                    | 默认 `RESTRICT`（业务层返回 409） |
| 文件被头像引用                    | 默认 `RESTRICT` 或先置空          |

- 业务删除优先软删除；确需硬删除时先检查引用并写审计。
- 所有 `ON DELETE` 行为必须在迁移中显式写出，不能依赖默认 `NO ACTION` 而无人知晓。

## 6. 索引基线

至少覆盖：

- `users`：`lower(username)`、`lower(email)` 部分唯一索引（`WHERE deleted_at IS NULL`）；`status`；`created_at`。
- `roles`：`code` 唯一；`is_system`。
- `user_roles`：`role_id`。
- `role_permissions`：`permission_code`。
- `user_sessions`：`user_id`、`expires_at`、`revoked_at`。
- `refresh_token_history`：`session_id`、`expires_at`。
- `audit_logs`：`created_at DESC`、`actor_id + created_at`、`action + created_at`、`resource + resource_id`、`request_id`。
- `files`：`uploader_id + created_at`、`sha256`、`status`。

- 搜索字段（用户名/邮箱/文件名）按查询模式加 `pg_trgm`/全文索引；不要对所有列盲目加索引。
- 大表建索引评估 `CREATE INDEX CONCURRENTLY`；迁移中执行时注意事务限制。

## 7. 迁移规则

- 使用 Goose，版本号递增；迁移文件只增不改，已应用的文件禁止编辑。
- 一个迁移只做一件事：建表、加列、加索引、回填分开；生产优先 expand/contract（先加可空列/新表，再回填，最后收紧约束）。
- 生产迁移 forward-only；`down` 仅用于本地开发/测试，且不能造成数据丢失。
- 破坏性变更（删列、改类型、加 NOT NULL）必须分批并写回滚/备份说明。
- 新表/列必须同步更新 `sqlc` 查询与生成代码；`sqlc generate` 产物提交，CI 用 `sqlc generate` + `git diff --exit-code` 检查漂移。
- 权限/系统角色 seed 通过启动同步或专用迁移；不要创建默认用户/默认密码。
- 迁移不得包含环境相关数据（真实邮箱、密钥、外部 URL）。

## 8. 并发与锁

- Setup：`pg_advisory_xact_lock` + `users == 0` 检查，保证并发只成功一次。
- 最后一名活跃超管：禁用/删除/移除角色时在同一事务内 `SELECT ... FOR UPDATE` 或 advisory lock，再统计活跃超管；不能先查后改。
- 角色删除/用户分配：同一事务内检查引用并删除，避免“检查时无用户、删除后用户被分配”。
- 唯一冲突交给数据库唯一索引，捕获后映射 `409`；不要用“先查再插”替代约束。
- 缓存失效在事务提交后执行；回滚时不能留下已失效/已写入的脏缓存。

## 9. 清理与保留

- `user_sessions` / `refresh_token_history`：定期删除过期/已吊销超过 N 天的记录；保留窗口可配置。
- `audit_logs`：默认保留 **180 天**（`audit.retention_days`，`0` 表示不自动清理），MVP 由 janitor 按批次（如 5000 行）删除；MVP 不分区，单表达千万级或按月归档时再迁移为按 `created_at` 月分区。
- `files`：清理 `pending` 超时记录与孤儿对象；删除失败进入补偿队列/重试。
- 软删除表：查询默认排除 `deleted_at is not null`；`users` 唯一索引采用 `WHERE deleted_at IS NULL` 部分唯一索引，其余软删除表按业务场景明确是否排除已删除行。
