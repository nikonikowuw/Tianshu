# Tianshu (天枢) — Agent Instructions

Tianshu 是一个单仓库全栈后台脚手架：`server/`（Go + Gin + PostgreSQL + sqlc + Goose）通过 `//go:embed` 嵌入 `web/`（Vite + React 19 + TypeScript + TanStack Router/Query/Table + Tailwind CSS v4 + shadcn/ui + Zustand），实现单二进制部署。

> 当前状态：`server/` 与 `web/` 基础框架底座、`access`（细粒度权限目录、RBAC 缓存中间件、角色 CRUD 与前端 `/roles` 权限矩阵页）、`setup`（首次初始化向导）、`auth`（登录/Refresh 轮换与重放检测/软冷却/JWT 鉴权）与 `audit`（脱敏审计落库、保留期清理与前端 `/audit-logs` 只读检索页）已落地；用户管理、`files`、个人中心/会话与 `dashboard` 待按垂直切片逐步实现。需求基线见 [docs/prd/prd.md](./docs/prd/prd.md)，所有技术与流程契约以 `.trellis/spec/` 为准。

## Spec 导航（动手前先读对应域）

- 总入口：[`.trellis/spec/index.md`](./.trellis/spec/index.md)
- 后端契约：[`.trellis/spec/backend/index.md`](./.trellis/spec/backend/index.md)（架构、Go 风格、API 契约、安全基线、数据模型、测试）
- 前端契约：[`.trellis/spec/frontend/index.md`](./.trellis/spec/frontend/index.md)（目录依赖、API/认证接线、UI/表单/i18n、服务端 DataTable、测试）
- 开发与评审：[`.trellis/spec/workflow/index.md`](./.trellis/spec/workflow/index.md)（功能分支 & Squash Merge PR、Conventional Commits、最多 3 轮循环 Review-Fix）
- 初始化参考：[`.trellis/research/shadcn-admin-bootstrap.md`](./.trellis/research/shadcn-admin-bootstrap.md)（一次性模板裁剪与上游升级说明）

## 全局不可协商红线

1. **分支、PR 与评审**：严禁直接 push `main`；必须切独立功能分支开发，经自动化门禁与最多 3 轮循环 `review-fix`（第 1 轮全量，第 2/3 轮定向复检）后，**强制 Squash Merge** 合入 `main` 并删除分支。
2. **后端架构与契约**：业务模块按 `identity`、`access`、`auth`、`audit`、`files`、`dashboard`、`setup` 纵向切分，严禁 `modules/*` 互导；事务只在 `service.go` 经 `TxManager.WithinTx` 开启；所有 DB 访问走 `sqlc`；成功响应无信封，错误体固定 `{ code, message, errors? }`，`X-Request-Id` 只走响应头。
3. **前端架构与体验**：Access Token 仅存内存；服务端状态走 TanStack Query；严禁跨 feature import、`components/ui/**` 零业务逻辑、`components/layout/**` 不直连 `auth-store`、路由守卫经 `RouterContext` 注入；组件复用 `shadcn-admin` 原生样式，可见文案全走 i18n。
4. **单二进制前端嵌入**：前端构建产物经 `server/Makefile` 复制进 `server/internal/webui/dist` 后由 `internal/webui` 嵌入；保留 `.gitkeep`、不提交构建产物、`/api/*` 404 不得被 SPA fallback 吞掉。
5. **生成文件禁手改**：`server/internal/modules/*/internal/dbgen/**`、`web/src/routeTree.gen.ts` 仅由工具生成并提交，禁止手工编辑。
6. **文档同轮次同步**：改动接口、表结构、安全策略、实现状态或目录边界时，在同一轮次更新受影响的 Spec 与 README。

## 验证与构建入口（随代码落地启用）

- **根目录 `Makefile`（薄封装）**：`make dev`、`make test`、`make build`（整包构建：先构建并嵌入前端再编译 Go）、`make lint`、`make docker`。
- **后端 `server/Makefile`（Go 快速开发与构建细节）**：`make dev`（`air` 热重载，不触发前端 embed）、`make test`、`make lint`、`make sqlc`、`make swagger`、`make web-build`、`make web-embed`、`make server-build`、`make build`。
- **前端 `web/`**：`pnpm dev`、`pnpm typecheck`、`pnpm lint`、`pnpm format:check`、`pnpm test`、`pnpm build`。
- 详细 CI 与校验顺序见 [`.trellis/spec/backend/testing.md`](./.trellis/spec/backend/testing.md) 与 [`.trellis/spec/frontend/testing.md`](./.trellis/spec/frontend/testing.md)。

<!-- TRELLIS:START -->

# mini-trellis

This project uses mini-trellis as a memory layer. Working knowledge lives under `.trellis/`:

- `.trellis/spec/` — durable contracts (short markdown; read `index.md` first)
- `.trellis/research/` — topic inbox (`<topic>.md`; `README.md` is not a topic)
- `.trellis/workspace/` — per-developer journals

Record a session with `/mini-trellis:remember` (Claude/OpenCode), `/mini-trellis-remember` (Pi/OMP/Kiro/Antigravity/Droid), `$mini-trellis-remember` (Codex/ZCode), or the `mini-trellis-remember` skill (Cursor and DeepSeek Harness). When earlier decisions or relevant project rules are needed, use `mini-trellis mem recall "<topic or path>"`; expand evidence with `mini-trellis mem read "<ref>"`. No search is required for unrelated work. Promote lasting boundaries into spec with `mini-trellis-update-spec`.

When the user confirms or corrects a project rule, save it through remember even without an explicit “remember” request, unless they ask not to save. An agreed future policy is recorded as agreed, not implemented. Also record reusable findings or a useful session/compact handoff. Questions, reviews, ordinary edits and unconfirmed proposals need no new record. New installs write notes without an automatic git commit; use `mini-trellis doctor` to check host wiring and `mini-trellis refresh --dry-run` before refreshing assets.

Keep one current rule in the document that already owns it. Preserve conditions with their rules: “when X, do not Y” must not become “never Y.” Unconfirmed scope, exceptions, and implementation choices stay open or are labeled as inference, not added to the contract.

For a confirmed update, use remember to edit the existing topic, verify the saved section once, record the journal link, and check the new entry/index row. Complete these authorized steps in the same turn without asking again between writes. Reuse returned file contents while unchanged instead of repeating reads. Journal-only success is not topic success; report partial writes accurately. New notes follow the requested or discussion language; updates preserve the document language.

For a memory-only update, reply with one short sentence using actual topic and journal Markdown links; expand for conflicts or partial writes. In a development task, include the links briefly with the requested result. Update affected README/docs/spec/research descriptions, including stale implementation status, in the same turn as the code. This maintenance needs no separate remember request or new journal entry.

When recall excerpts already answer the question with source and qualifiers, cite them and stop. Expand only missing context; do not repeat a lookup or fall back to dialogue-only `search` merely to reconfirm sufficient evidence.

Use project knowledge or supply Markdown/text exports when working with earlier ZCode, Kiro, OMP, or Antigravity conversations. To organize selected past sessions or supplied exports into project knowledge, use `mini-trellis-session-insight`, update existing documents, and preserve sources.

mini-trellis does not create or drive tasks.

Managed by mini-trellis. Edits outside this block are preserved.
<!-- TRELLIS:END -->
