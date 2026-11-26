# Spec Index

Tianshu 的持久契约入口。当前已完成 `server/` 与 `web/` 基础框架底座（平台底座、前端壳与单二进制构建）；业务模块尚未实现，以下文档仍是目标契约而非全量已完成能力。

| 领域     | 索引                                     | 内容                                                          |
| -------- | ---------------------------------------- | ------------------------------------------------------------- |
| Backend  | [backend/index.md](./backend/index.md)   | Go 架构、风格、API、安全、数据模型、测试                      |
| Frontend | [frontend/index.md](./frontend/index.md) | 目录与依赖、API/认证接线、UI/i18n、服务端 DataTable、测试门禁 |
| Workflow | [workflow/index.md](./workflow/index.md) | 功能分支与 PR（Squash Merge）、Git commit、Code Review        |
| Guides   | [guides/index.md](./guides/index.md)     | mini-trellis 记忆层与维护方式                                 |

阅读顺序建议：`index` → 对应领域的 `index` → 具体文档。跨领域改动至少同时读 Backend/Frontend 的 API 契约与 Workflow。
