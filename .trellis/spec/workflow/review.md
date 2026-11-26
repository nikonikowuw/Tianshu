# Code Review Workflow

本规范定义 Tianshu 的 Code Review 门禁、双轴评审流程与缺陷分级标准，适用于人工互审、作者自审与 AI 辅助评审。

前置依赖：功能分支、PR 模板与 Squash Merge 流程见 [pr.md](./pr.md)，提交格式与原子提交规则见 [git-commit.md](./git-commit.md)。

## 1. 合并门禁（Merge Gate）

本项目采用**个人 + AI 协作优先，兼容多人团队**的门禁模式：

- **基础必选条件（任何模式均不可跳过）**：
  1. 自动化门禁全绿（CI 或等价本地完整验证通过）。
  2. 完成双轴评审（Spec + Standards），所有 `Blocker` 级问题已修复并复检。
  3. 禁止直接向 `main` push；通过 PR/合并请求合入。
- **个人 / AI 协作模式**：满足基础必选条件后，作者复核 AI 评审结论并确认无遗留 `Blocker` 即可合并。
- **多人协作模式**：在基础必选条件之上，还须获得至少 1 名非作者维护者的 Approval 方可合并，禁止自审合并。
- **AI 评审边界与写审分离**：
  - AI 评审用于辅助发现架构越界、契约漂移、安全隐患与边界遗漏，不能替代运行测试或绕过失败的 CI。
  - **强制独立 Subagent 上下文（写审分离）**：禁止由刚编写代码的同一主会话上下文直接原地自审。主 Agent 仅负责运行自动化前置门禁与编排修复，实质双轴评审必须委托给**独立上下文的只读 Reviewer Subagent**（仅传入变更范围、门禁实测输出与必读 Spec 路径，隔离编码过程假设）；第 2、3 轮定向复检复用该独立 Reviewer Subagent（或仅传入上一轮问题清单与修复改动），保持评审客观并避免污染主上下文。

## 2. PR 准备与规模边界

- **按单一逻辑单元切分（不设固定行数上限）**：适配现代 Agent 辅助开发场景，不按固定代码行数硬切 PR；只要变更聚焦同一目标且能独立编译、测试与回滚，即视为合理范围，严禁在单 PR 中混杂无关功能、无关依赖升级或大面积纯格式化。
- **自然分层与大变更导读**：跨端/超大型需求能自然拆分为可独立交付阶段（如“迁移与后端模块”与“前端页面与 E2E”）时优先拆分；确需单 PR 闭环的大变更，必须在 PR 描述中附**评审导读（Review Guide）**，明确标出核心高风险文件（迁移 SQL、API 契约、权限/安全、并发与状态机）与常规模板/测试文件，由 CI 与 AI 初审先兜底机械项，人工聚焦核心边界。
- **PR 描述与原子完整性**：按 [pr.md](./pr.md) 第 2 节填写描述（动机、取舍、影响范围、验证结果）；并遵守 [git-commit.md](./git-commit.md) 第 5 节，确保迁移 SQL、`queries.sql`、`sqlc` 产物、路由生成文件及受影响的 spec/README 同步包含。

## 3. 自动化前置检查

能由机器检查的问题不占用人工 Review 时间。发起评审前，作者先确保本地/CI 通过（详细命令见 [backend/testing.md](../backend/testing.md) 与 [frontend/testing.md](../frontend/testing.md)）：

- **Backend**：`gofmt`、`go vet`、`golangci-lint`、`go test -race ./...`、`sqlc generate && git diff --exit-code`、`go build ./...`；涉及 embed 时执行 `make build`。
- **Frontend**：`pnpm typecheck`、`pnpm lint`、`pnpm format:check`、`pnpm test`、`pnpm build`；涉及核心链路时跑 Playwright E2E。
- **仓库卫生**：`git diff --check` 无空白错误；无密钥、`.env`、`config.local.toml`、`web/dist`、`server/internal/webui/dist/*`（除 `.gitkeep`）误入提交。

## 4. 循环 Review-Fix 流程（上限 3 轮）

评审与修复采用有上限的迭代循环，防止发散与无限返工——**单个 PR 的 `review-fix` 循环最多不超过 3 次**：

| 轮次                    | 评审范围                              | 关注重点                                                                                  |
| ----------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------- |
| **第 1 轮（全量评审）** | 本次 PR 的**全部变更内容**            | 完整执行双轴评审（Spec + Standards），一次性暴露所有 `Blocker`、`Suggestion` 与必要 `Nit` |
| **第 2 轮（定向复检）** | **第 1 轮提出的问题**及对应的修复改动 | 核验问题是否彻底修复、是否引入新回归；不重复重扫未改动区域发散新意见                      |
| **第 3 轮（最终复检）** | **第 2 轮残留的问题**及对应的修复改动 | 最终收敛核验；只看修复点及直接受影响边界                                                  |

收敛与退出规则：

- 任意一轮修复并复检后，若已无未解决的 `Blocker` 且自动化检查全绿，立即结束循环进入合并。
- 每一轮修复后都必须重跑受影响的测试与静态检查，不能只改代码不验证。
- 若完成第 3 轮后仍存在未解决的 `Blocker`，**禁止继续机械进入第 4 轮循环**——必须暂停并升级为人工介入，重新审视需求理解、架构方案或拆分 PR 后再重启。

## 5. 双轴评审（Spec + Standards）

评审沿两条独立主线展开，避免只看代码细节而漏掉契约偏离：

### 轴 A：契约与需求一致性（Spec Axis）

1. **需求与行为匹配**：改动是否完整覆盖目标需求/验收条件，有无遗漏边界状态或错误分支。
2. **HTTP 与错误契约**（[backend/api-contract.md](../backend/api-contract.md)）：无信封响应、空切片为 `[]`、`400` 与 `422` 语义准确、`X-Request-Id` 只走响应头、已发布错误码不静默改名或改义。
3. **安全与并发**（[backend/security.md](../backend/security.md)、[backend/data-model.md](../backend/data-model.md)）：认证/权限边界在后端兜底、无敏感信息进入日志或响应、唯一约束靠 DB 索引而非“先查再插”、并发写操作有事务与锁保护、缓存失效在事务提交后执行。
4. **文档同步**：接口、表结构、安全策略或实现状态发生变化时，对应 spec/README 是否在同一变更中同步更新；未确认的决策不得擅自写死进契约。

### 轴 B：架构与工程规范（Standards Axis）

1. **后端结构与风格**（[backend/architecture.md](../backend/architecture.md)、[backend/go-style.md](../backend/go-style.md)）：
   - 包依赖严格无环，`modules/*` 互不 import，`platform/*` 不依赖 `modules/*`；
   - `store` 私有化，不把 `dbgen` 结构体直接暴露给 HTTP；
   - `service.go` 只返回 `apperr.Error`，`handler.go` 统一 `c.Error(err)`；
   - 错误只处理一次，无发后即忘 Goroutine，无手改 `dbgen`。
2. **前端结构与规范**（[frontend/architecture.md](../frontend/architecture.md)、[frontend/api-auth.md](../frontend/api-auth.md)、[frontend/ui-conventions.md](../frontend/ui-conventions.md)、[frontend/data-table.md](../frontend/data-table.md)）：
   - Access Token 仅存内存；服务端状态走 TanStack Query，禁止组件内 `useEffect` 手写 fetch 或绕过统一 HTTP 客户端；
   - `features/<A>` 不直接 import `features/<B>`；`components/ui/**` 零业务逻辑；`components/layout/**` 不直连 `auth-store`；路由守卫经 `RouterContext` 注入；
   - 服务端列表分页/筛选/排序状态以 URL 为唯一事实源；
   - 可见文案走 i18n，样式走语义 token，无手改 `routeTree.gen.ts`。
3. **测试质量**（[backend/testing.md](../backend/testing.md)、[frontend/testing.md](../frontend/testing.md)）：覆盖成功、失败、边界与权限分支；DB 集成测试使用真实 PostgreSQL；无固定 `sleep` 与无断言凑覆盖率测试。

## 6. 按改动路径快速定位检查重点

| 改动路径                                             | 必读 Spec                                                                                                                                               | 重点检查项                                                                                   |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `server/migrations/**`、`queries.sql`                | [data-model.md](../backend/data-model.md)、[testing.md](../backend/testing.md)                                                                          | UUIDv7、`ON DELETE` 显式声明、索引基线、forward-only 迁移、`sqlc` 无漂移                     |
| `server/internal/modules/**`                         | [architecture.md](../backend/architecture.md)、[api-contract.md](../backend/api-contract.md)、[go-style.md](../backend/go-style.md)                     | 固定文件模板、跨模块 `deps.go`、事务边界、错误码与 i18n Key 成对完整                         |
| 认证 / RBAC / 文件 / 中间件                          | [security.md](../backend/security.md)、[api-contract.md](../backend/api-contract.md)                                                                    | 权限校验、Refresh Token 轮换与重放、限流、MIME 嗅探、审计日志与脱敏                          |
| `server/internal/webui/**`、`Makefile`、`Dockerfile` | [architecture.md](../backend/architecture.md) 第 6 节                                                                                                   | `web-embed` 复制链路、`.gitkeep` 保留、`/api/*` 不被 SPA 吞掉、缓存与安全头                  |
| `web/src/features/**`、`web/src/routes/**`           | [frontend/architecture.md](../frontend/architecture.md)、[data-table.md](../frontend/data-table.md)、[ui-conventions.md](../frontend/ui-conventions.md) | `RouterContext` 守卫顺序、`validateSearch` `.catch()`、服务端表格、`<PermissionGuard>`、i18n |
| `web/src/lib/api/**`、`web/src/stores/**`            | [frontend/api-auth.md](../frontend/api-auth.md)、[security.md](../backend/security.md)                                                                  | 内存 Token、单飞刷新、401 与 403（禁用 vs 无权限）分支、`X-Request-Id` 透传                  |

## 7. 评审意见分级

所有 Review 评论必须标注明确级别，聚焦事实与可执行方案，不在 Review 中争论已被 formatter/linter 覆盖的纯风格问题：

| 级别         | 含义                                    | 处理要求                                                                | 典型示例                                                                                                   |
| ------------ | --------------------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `Blocker`    | 阻塞合入的缺陷或红线违背                | **必须修复**并重新验证后才可合并                                        | 安全漏洞、权限绕过、并发竞态/数据不一致、破坏 API 契约、越界依赖、手改生成文件、缺少核心测试、提交敏感配置 |
| `Suggestion` | 设计、可读性、健壮性或性能改进          | 建议在当前 PR 修改；若范围较大，双方同意后转 follow-up Issue 并注明原因 | 抽取重复逻辑、优化 SQL 查询/索引、补充次要边界用例、改善组件拆分                                           |
| `Nit`        | 局部表达、命名或注释微调（前缀 `nit:`） | **不阻塞合并**，由作者自行决定是否采纳                                  | 局部变量命名优化、注释润色、细小结构整理                                                                   |

## 8. 评审收尾

1. 所有 `Blocker` 关闭且最终 commit 通过 CI 后，按 [pr.md](./pr.md) 第 1、3 节执行 rebase 同步与**强制 Squash Merge** 合入 `main`，整理压缩提交信息后删除功能分支。
2. 评审中如果新确认了持久边界、排除了不可行方案或修正了既有规范，按 [guides/mini-memory.md](../guides/mini-memory.md) 更新对应 Spec/文档并记录 session journal。
