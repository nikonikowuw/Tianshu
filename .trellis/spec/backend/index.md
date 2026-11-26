# Backend Spec

适用范围：`server/`（Go 服务）。当前已完成 `internal/platform/*`（含 Argon2id/Token 安全原语）、`internal/shared/*`、`internal/webui`、`migrations/00001_init_schema.sql`、`internal/app` 及 `internal/modules/access` 权限种子与 RBAC 校验底座；角色 CRUD 接口及 `setup`/`auth`/`identity`/`audit`/`files`/`dashboard` 模块尚未实现。

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [architecture.md](./architecture.md) | 目录、依赖方向、运行时契约、模块模板、前端 embed | 新增模块、改装配/路由/构建 |
| [go-style.md](./go-style.md) | 命名、接口、错误、并发、注释、测试风格 | 写/审 Go 代码 |
| [api-contract.md](./api-contract.md) | URL、状态码、分页、错误体、校验、请求 ID、版本 | 新增或修改接口 |
| [security.md](./security.md) | 密码、JWT、Refresh Token、Cookie、CSRF/CORS、限流、Setup、文件、审计 | 认证/安全相关改动 |
| [data-model.md](./data-model.md) | 表所有权、UUIDv7、时间/FK、索引、迁移、删除与保留 | 建表、迁移、写 SQL |
| [testing.md](./testing.md) | 单测、DB 集成测试、HTTP 契约、E2E、架构/i18n 校验 | 写测试、接 CI |

前端如何消费这些接口，见 [frontend/api-auth.md](../frontend/api-auth.md)。

## 不可协商

- 成功响应无信封；列表固定 `{ items, total, page, pageSize }`；空切片输出 `[]`。
- 错误统一 `{ code, message, errors? }`；`request_id` 只走 `X-Request-Id` 响应头，错误体不重复；`errors` 只用于 HTTP `422`，禁止用 `200` 返回业务错误。
- `service.go` 只返回 `apperr.Error`；`handler.go` 只 `c.Error(err)`；HTTP 状态与文案由 `ErrorHandler` 收口。
- 所有写操作通过目标模块 `Service`；事务只在 `service.go` 用 `TxManager.WithinTx` 开启。
- 密码 `argon2id`；Refresh Token 只存 SHA-256 哈希；Access Token 只放前端内存。
- 所有数据库访问走 `sqlc` 参数化查询；动态排序字段必须白名单。
- 前端构建产物通过 `internal/webui` 的 `//go:embed all:dist` 嵌入；不在 Go 代码里跨目录引用 `web/dist`。
- 生成代码不手改：`internal/modules/*/internal/dbgen/**`、`web/src/routeTree.gen.ts`。
