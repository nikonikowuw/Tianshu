---
name: tianshu-review
description: >-
  Execute Tianshu's spec-driven Code Review workflow (.trellis/spec/workflow/review.md)
  using an isolated Reviewer Subagent context (write-review separation): automated
  preflight gates, up to 3-round Review-Fix convergence, dual-axis review (Spec Axis +
  Standards Axis), path-based contract checks, and Blocker/Suggestion/Nit grading.
  Use when the user asks to review code, self-review before opening or merging a PR,
  check branch/working-tree changes against Tianshu specs, or run a review-fix loop.
---

# Tianshu Code Review Skill (`tianshu-review`)

按 Tianshu 评审契约 [review.md](../../../.trellis/spec/workflow/review.md) 执行标准化代码评审。本 Skill 是评审编排器，持久规则以 `.trellis/spec/` 为唯一事实源。

> **适用边界**：仅对涉及 `server/`、`web/`、SQL 迁移、构建/部署配置等**实际代码与运行逻辑变更**启用独立 Subagent 评审；纯文档、记忆层记录（`.trellis/`）或 `.agents/` 技能维护无需拉起代码评审 Subagent。

## 核心红线：强制独立 Subagent 上下文（写审分离）

1. **严禁主会话原地自审**：主 Agent 仅负责编排（探查范围、执行 `preflight.sh` 自动化门禁、汇总报告，以及在闭环模式下修复代码并重跑验证），**严禁在主会话上下文中自己审自己刚写的代码**。
2. **强制派发只读 `research` Subagent**：所有代码与契约的实质双轴审查，必须通过 `invoke_subagent`（`TypeName: "research"`）在**全新独立上下文**中执行（只读沙盒、不共享主会话编码历史，消除作者确认偏误并保护主会话上下文容量）。
3. **跨轮次复用独立评审会话**：在 Review-Fix 循环的第 2、3 轮定向复检中，通过 `send_message` 复用第 1 轮创建的 Reviewer Subagent `conversationId`（仅输入上一轮问题点、本次修复改动与最新门禁结果），保持复检上下文聚焦且连续。

## 评审模式判定

1. **只读评审模式（默认）**：当用户要求“review / 代码评审 / 提交前自检 / 按 spec 检查”时，主 Agent 跑完门禁并采集独立 Reviewer Subagent 结论后输出结构化报告，**不擅自修改业务代码**。
2. **闭环修复模式（仅在用户明确要求“review 并修复 / 跑 review-fix”时启用）**：按 `Round 1 独立全量评审 → 主 Agent 修复 Blocker（及合理 Suggestion）并重跑门禁 → Round 2 独立定向复检 → ...` 推进，严格遵守 **最多 3 轮上限**。

---

## 执行步骤

### Step 1：确定评审范围、分支基线与轮次（主 Agent）

1. **确定变更范围**（若用户未指定，默认覆盖当前分支相对 `origin/main` 的全部变更 + 当前工作区未提交改动；若为基于未合并前置分支切出的 stacked branch 或仅评审工作区未提交改动，可传 `--base <parent-branch>` 或 `--base HEAD`）：
   ```bash
   bash .agents/skills/tianshu-review/scripts/preflight.sh --mode scope [--base <ref>]
   ```
   - 核对分支红线（[pr.md](../../../.trellis/spec/workflow/pr.md)）：禁止直接在 `main` 上开发或准备直接 push `main`；若当前处于 `main` 且存在业务改动，直接记为 `Blocker`。
2. **确定评审轮次（上限 3 轮）**：
   - **Round 1（全量评审，默认）**：对目标范围内的**全部变更**执行完整双轴评审（Spec + Standards），一次性暴露所有 `Blocker`、`Suggestion` 与必要 `Nit`。
   - **Round 2（定向复检）**：**仅检查第 1 轮提出的问题及对应的修复改动**，核验是否彻底解决、是否引入新回归；**禁止对未改动区域重扫发散新意见**。
   - **Round 3（最终复检）**：**仅检查第 2 轮残留问题及修复点直接受影响边界**，做最终收敛核验。
   - **超限熔断**：若第 3 轮复检后仍有未解决的 `Blocker`，**严禁进入第 4 轮循环**——立即停止并输出人工升级建议（重新对齐需求、调整架构方案或拆分 PR）。

### Step 2：运行自动化前置门禁（主 Agent）

由于 `research` Subagent 运行在只读沙盒中不执行构建/测试命令，主 Agent 必须在本地先运行门禁并将**真实原始结果**传递给 Reviewer Subagent：

```bash
bash .agents/skills/tianshu-review/scripts/preflight.sh --mode full [--base <ref>]
```

> 若环境缺少外部依赖（如本地未启动 PostgreSQL 导致 DB 集成测试跳过或失败），必须在门禁章节如实记录实际输出与未覆盖项，严禁伪造“全绿”。
> 涉及 `queries.sql` 改动时，额外校验 `cd server && sqlc generate` 后 `dbgen` 无未同步漂移；涉及核心前端交互链路时，按 [frontend/testing.md](../../../.trellis/spec/frontend/testing.md) 核验或提示补充 Playwright E2E。

### Step 3：计算必读 Spec 清单（按改动路径映射）

根据 Step 1 识别出的改动路径，汇总需要交给 Reviewer Subagent 阅读的契约文件路径（始终包含 [review.md](../../../.trellis/spec/workflow/review.md)，未触及的领域无需加入）：

| 命中路径 | 必读契约文档 |
| --- | --- |
| `server/migrations/**`、`**/queries.sql` | [backend/data-model.md](../../../.trellis/spec/backend/data-model.md)、[backend/testing.md](../../../.trellis/spec/backend/testing.md) |
| `server/internal/modules/**`、`server/internal/app/**`、`server/internal/platform/**` | [backend/architecture.md](../../../.trellis/spec/backend/architecture.md)、[backend/api-contract.md](../../../.trellis/spec/backend/api-contract.md)、[backend/go-style.md](../../../.trellis/spec/backend/go-style.md) |
| 认证 / RBAC / 用户状态 / 文件 / 审计 / 中间件 | [backend/security.md](../../../.trellis/spec/backend/security.md)、[backend/api-contract.md](../../../.trellis/spec/backend/api-contract.md) |
| `server/internal/webui/**`、`Makefile`、`server/Makefile`、`server/Dockerfile` | [backend/architecture.md](../../../.trellis/spec/backend/architecture.md)（第 6 节前端嵌入） |
| `web/src/features/**`、`web/src/routes/**`、`web/src/components/**` | [frontend/architecture.md](../../../.trellis/spec/frontend/architecture.md)、[frontend/data-table.md](../../../.trellis/spec/frontend/data-table.md)、[frontend/ui-conventions.md](../../../.trellis/spec/frontend/ui-conventions.md) |
| `web/src/lib/api/**`、`web/src/lib/auth/**`、`web/src/stores/**` | [frontend/api-auth.md](../../../.trellis/spec/frontend/api-auth.md)、[backend/security.md](../../../.trellis/spec/backend/security.md) |
| `*_test.go`、`*.test.ts`、`*.test.tsx`、`e2e/**` | [backend/testing.md](../../../.trellis/spec/backend/testing.md)、[frontend/testing.md](../../../.trellis/spec/frontend/testing.md) |
| 提交拆分 / PR 描述 / 文档同步 | [workflow/pr.md](../../../.trellis/spec/workflow/pr.md)、[workflow/git-commit.md](../../../.trellis/spec/workflow/git-commit.md) |

### Step 4：派发独立 Reviewer Subagent 执行双轴评审

#### 4.1 派发策略
- **Round 1 常规变更（单文件至中等模块改动）**：调用 `invoke_subagent`（`TypeName: "research"`, `Role: "Tianshu Independent Reviewer"`）启动 **1 个独立 Subagent** 完整执行轴 A + 轴 B 审查。
- **Round 1 大体积跨端变更（例如全栈垂直切片同时涉及后端多文件 + 前端多组件）**：并行调用两次 `invoke_subagent`（`TypeName: "research"`）：
  1. `Role: "Spec & Security Reviewer"` —— 专审 **轴 A（契约与需求一致性、安全并发、文档同步）**；
  2. `Role: "Architecture & Standards Reviewer"` —— 专审 **轴 B（前后端架构边界、编码规范、测试质量）**。
- **Round 2 / Round 3 定向复检**：调用 `send_message` 向第 1 轮对应的 Reviewer Subagent 发送复检请求，仅附带：**上一轮未关闭问题清单 + 本轮修复涉及的文件与改动说明 + 最新门禁输出**，并明确声明“当前为第 X 轮定向复检，只验证上述问题修复与直接回归，禁止重扫未改动区域发散新意见”。

#### 4.2 传递给 Reviewer Subagent 的客观 Prompt 要素
向 Subagent 传递 Prompt 时**只给客观事实与检查契约，严禁附带主会话的辩解或预设“应该没问题”的引导语**：
1. 当前分支、对比基线 `merge-base`、全部已变更/新增文件路径清单（提示 Subagent 直接读取这些源码文件及上下文做核查）。
2. Step 2 的自动化门禁实测结果摘要。
3. Step 3 命中的 `.trellis/spec/**`（必要时含 [docs/prd/prd.md](../../../docs/prd/prd.md)）绝对路径清单，要求先读 Spec 再审代码。
4. 双轴审查重点与三级分级规则（见下节），要求每条问题必须给出**绝对路径 + 精确行号范围 + 违反的具体 Spec 条款 + 可执行修复方案**。

#### 4.3 双轴审查清单（Reviewer Subagent 执行基准）

- **轴 A：契约与需求一致性（Spec Axis）**
  1. **需求与行为闭环**：正常路径、边界值、空状态、禁用态、幂等与错误分支是否齐全。
  2. **HTTP 与错误契约**：无信封响应；分页固定 `{ items, total, page, pageSize }` 且空切片为 `[]`；错误体固定 `{ code, message, errors? }`（`errors` 仅用于 `422`）；`400` vs `422` 语义准确；禁用 `200` 装业务错误；`X-Request-Id` 只走响应头；已发布错误码不静默改名或改义。
  3. **安全、事务与并发**：后端强制鉴权与权限码兜底；密码 `argon2id`、Refresh Token 仅存 SHA-256 哈希；日志/审计/响应无敏感信息泄露；唯一性靠 DB 唯一索引而非无锁“先查再插”；跨写操作经 `service.go` `TxManager.WithinTx` 编排；最后一名超管保护/Token 轮换/Setup 具备行锁或原子并发保护；**RBAC/会话缓存失效必须在事务提交成功后执行**。
  4. **原子性与文档同轮次同步**：迁移 SQL + `queries.sql` + `dbgen`、路由 + `routeTree.gen.ts`、错误码 + `zh-CN`/`en-US` 语言包成套齐全；接口/表结构/安全策略/实现状态变更时，`AGENTS.md`、`README` 与 `.trellis/spec/**` 同轮次同步更新，未确认提案不擅自写入契约。

- **轴 B：架构与工程规范（Standards Axis）**
  1. **后端架构与 Go 规范**：`modules/*` 严禁互导（跨模块经消费方 `deps.go` 窄接口 + `internal/app` 适配）；`platform/*` 不引 `modules/*`；`store` 与 `dbgen` 严格私有不透出 HTTP；`service.go` 只返 `*apperr.Error`，`handler.go` 只 `c.Error(err)`；动态排序字段显式白名单；无裸 goroutine；禁手改 `dbgen/**`。
  2. **前端架构与 UI/状态规范**：Access Token 仅存内存；服务端状态全走 TanStack Query + 统一 HTTP 客户端，禁组件内 `useEffect` 手写 `fetch`；严禁跨 `features/*` import；`components/ui/**` 零业务逻辑；`components/layout/**` 不直连 `auth-store`；路由守卫经 `RouterContext` 注入；服务端表格分页/筛选/排序以 URL 为唯一事实源，`validateSearch` 带 `.catch()` 容错；全量可见文案走 i18n（中英同步）；`web/vite.config.ts` 保持 `headless: true`；禁手改 `routeTree.gen.ts`。
  3. **测试质量**：具备对应单测、真实 PostgreSQL 集成测试（涉及 SQL/约束/事务时）、HTTP 契约测试与前端组件测试；禁固定 `Sleep` / `waitForTimeout`、禁硬编码单一语言完整字符串断言、禁共享污染全局状态或无断言测试。

### Step 5：主 Agent 交叉核验并输出分级报告

主 Agent 收到独立 Reviewer Subagent 的返回后，快速核对被引用的源码行号确实存在且与门禁结果一致（剔除纯幻觉或已被 formatter/linter 覆盖的纯风格争论），按三级标准输出报告：

- **`Blocker`**：阻塞合入的缺陷或红线违背（安全漏洞、权限绕过、并发竞态/事务或缓存时序错误、破坏 API 契约、模块越界 import、手改生成文件、遗漏核心测试、自动化门禁失败、提交敏感文件、漏更受影响 Spec）。**必须修复并复检通过后方可合并**。
- **`Suggestion`**：结构、可读性、健壮性、SQL/索引或次要边界测试改进。建议当前 PR 处理，范围较大时可注明转 follow-up。
- **`Nit`**：局部命名、注释或微调（标题以 `nit:` 开头），**不阻塞合并**。

标准报告模板（所有文件引用使用可点击 `[basename](file:///abs/path#Lstart-Lend)` 链接）：

```markdown
## Tianshu Code Review 报告（Round <1|2|3> / 3）

- **分支与范围**：`<branch>` vs `<base>`（共 X 个文件）
- **独立评审会话**：`[Reviewer](conversation://<subagent-id>)`（如拆轴则分别列出轴 A / 轴 B 会话链接）
- **评审导读**（大变更时附上）：高风险核心文件 vs 常规文件

### 1. 自动化门禁（Automated Gates）
- 仓库卫生与禁入文件：PASS / FAIL（说明）
- Backend 门禁：PASS / FAIL / N/A（说明）
- Frontend 门禁：PASS / FAIL / N/A（说明）
- Embed 构建门禁：PASS / FAIL / N/A（说明）

### 2. 评审发现（Findings）

#### Blocker（必须修复）
1. **[Blocker][轴 A/B] <问题简述>**
   - 位置：[file.go](file:///abs/path/to/file.go#L10-L25)
   - 依据：违背 `<spec 文档与条款>` —— <具体事实与风险说明>
   - 建议修复：<明确可执行方案>

#### Suggestion（建议改进）
- ...（无则写“无”）

#### Nit（可选微调）
- `nit:` ...（无则写“无”）

### 3. 合并门禁结论（Merge Gate Verdict）
- **结论**：`APPROVED` | `CHANGES REQUESTED (进入 Fix -> Round X+1)` | `ESCALATED (触发 3 轮上限，需人工介入)`
- **下一步行动**：...
```

### Step 6：收尾指引

- 当结论为 `APPROVED`（门禁全绿且 `0 Blocker`）时，提示按 [pr.md](../../../.trellis/spec/workflow/pr.md) 同步 `origin/main`（`git rebase origin/main`）、整理符合 [git-commit.md](../../../.trellis/spec/workflow/git-commit.md) 的压缩提交信息，并通过 **强制 Squash Merge** 合入 `main` 后删除功能分支。
- 若评审过程中确认了新的项目持久规则、排除了不可行方案或修正了既有规范，按 `mini-trellis-remember` / `mini-trellis-update-spec` 更新对应文档并记录 journal（纯代码评审本身无需写入 journal）。
