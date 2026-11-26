# Spec Index

Tianshu 的持久契约入口。当前已完成 `server/` 与 `web/` 基础框架底座（平台底座、前端壳与单二进制构建）以及 `access`（RBAC 底座与角色/权限管理）、`setup`、`auth` 与 `audit` 垂直切片；其余业务模块（用户管理、`files`、个人中心与 `dashboard`）仍在按切片推进，以下文档作为目标契约持续对齐。

| 领域     | 索引                                     | 内容                                                          |
| -------- | ---------------------------------------- | ------------------------------------------------------------- |
| Backend  | [backend/index.md](./backend/index.md)   | Go 架构、风格、API、安全、数据模型、测试                      |
| Frontend | [frontend/index.md](./frontend/index.md) | 目录与依赖、API/认证接线、UI/i18n、服务端 DataTable、测试门禁 |
| Workflow | [workflow/index.md](./workflow/index.md) | 功能分支与 PR（Squash Merge）、Git commit、Code Review        |
| Guides   | [guides/index.md](./guides/index.md)     | mini-trellis 记忆层与维护方式                                 |

阅读顺序建议：`index` → 对应领域的 `index` → 具体文档。跨领域改动至少同时读 Backend/Frontend 的 API 契约与 Workflow。
