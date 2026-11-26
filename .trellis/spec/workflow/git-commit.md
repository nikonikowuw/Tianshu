# Git Commit Convention

采用 [Conventional Commits 1.0.0](https://www.conventionalcommits.org/) 的 header 结构，结合本仓库的中文文档习惯。

## 1. 格式

```text
<type>(<scope>): <subject>

<body>

<footer>
```

- `type` / `scope` 固定英文小写；`subject` 默认中文，技术标识保留英文。
- `subject` 使用祈使句，不加句号，不超过 72 字符。
- `body` 说明“为什么/影响/边界”，不要复述 diff；可中文，建议每行不超过 72 字符。
- header 与 body、body 与 footer 之间各留一个空行。
- 破坏性变更在 type/scope 后加 `!`，并在 footer 写 `BREAKING CHANGE:`。
- 一个仓库只使用一种 subject 语言；如果团队决定全英文，整体切换，不要同一 PR 混用。

正则基线：

```text
^(feat|fix|refactor|perf|docs|test|build|ci|chore|style|revert)(\([a-z0-9-]+\))?!?: .{1,72}$
```

## 2. Type

| type       | 用途                                      |
| ---------- | ----------------------------------------- |
| `feat`     | 用户可见的新功能/接口                     |
| `fix`      | 修复缺陷                                  |
| `refactor` | 不改变外部行为的重构                      |
| `perf`     | 性能优化                                  |
| `docs`     | README、PRD、spec、注释、文档站           |
| `test`     | 新增/修正测试                             |
| `build`    | Makefile、Docker、embed、构建脚本、工具链 |
| `ci`       | CI/CD 配置                                |
| `chore`    | 依赖、杂务、生成文件、仓库维护            |
| `style`    | 纯格式化，不改逻辑                        |
| `revert`   | 回滚既有提交                              |

## 3. Scope

按影响面选择，不写 `all`、`misc` 这类无意义 scope。

| scope                                               | 范围                                    |
| --------------------------------------------------- | --------------------------------------- |
| `server`                                            | Go 服务整体、启动/装配                  |
| `web`                                               | 前端整体、页面/组件                     |
| `api`                                               | HTTP 契约、路由、DTO                    |
| `auth`                                              | 登录、JWT、Refresh Token、会话          |
| `identity`                                          | 用户与个人中心（`users` / `profile`）   |
| `access`                                            | 角色、权限、user_roles/role_permissions |
| `users` / `roles` / `audit` / `files` / `dashboard` | 对应前端页面或业务域                    |
| `setup`                                             | 首次初始化                              |
| `security`                                          | 安全基线、限流、CORS、CSRF              |
| `db`                                                | 迁移、索引、sqlc                        |
| `embed`                                             | 前端 embed、静态资源、Makefile          |
| `i18n`                                              | 前后端文案/语言包                       |
| `deps`                                              | 依赖升级                                |
| `docs` / `spec`                                     | 文档/规范                               |
| `ci` / `release`                                    | CI、版本发布                            |
| `trellis`                                           | `.trellis/` 记忆、journal、研究         |

## 4. Body 与 Footer

Body 回答：

- 为什么需要这个改动；
- 关键取舍/边界条件；
- 对 API、数据、部署、前端的影响。

Footer：

```text
BREAKING CHANGE: 列表响应新增 page/pageSize，旧客户端需同步升级。
Refs: #123
Closes: #456
```

- issue 编号放 footer，不塞进 subject。
- 多个 trailer 每行一个。
- 没有 footer 就省略，不要写空模板。

## 5. 原子提交

一个 commit 只表达一个逻辑变更，尽量保证单独 checkout 后仍能构建/测试。

必须放在同一个 commit：

- 迁移 SQL + `queries.sql` + `sqlc` 生成代码 + 对应模块代码 + 测试。
- 路由变更 + `routeTree.gen.ts` + 页面/权限守卫。
- 依赖升级 + `go.mod`/`go.sum` 或 `package.json`/`pnpm-lock.yaml` + 必要适配代码。
- embed 变更 + `server/Makefile` + `internal/webui` + `Dockerfile` + 相关 spec。
- 行为变更 + 对应 README/spec 更新（也可拆成相邻 commit，但不能只改代码不改文档）。

应当拆开：

- 纯格式化/重命名 vs 行为变更。
- 无关依赖升级 vs 业务功能。
- 大规模重构 vs 新功能。
- 生成代码批量更新 vs 手工逻辑修改（除非生成源同步变更）。
- 文档/规范整理 vs 运行时代码。

## 6. 禁止提交

- 密钥、Token、Cookie、真实 `.env`、`config.local.toml`。
- `web/dist/`、`server/internal/webui/dist/*`（只允许 `.gitkeep`）。
- `node_modules/`、`bin/`、coverage、日志、`.DS_Store`、临时文件。
- `.trellis/.developer`、`.trellis/.runtime/`、journal 运行时锁。
- 宿主专属运行目录（如 `.dsh/`、编辑器缓存）默认不提交；若团队决定纳入版本管理，单独用 `chore(agent)`/`docs(trellis)` 提交，不要混业务代码。
- 生成代码的手工修改：`internal/modules/*/internal/dbgen/**`、`web/src/routeTree.gen.ts`。

## 7. 提交前检查

快速检查（每次 commit）：

```bash
gofmt -l .
go vet ./...
go test ./...            # 至少跑受影响包
pnpm --dir web typecheck
pnpm --dir web lint
```

涉及以下改动时追加：

| 改动                      | 追加检查                                                |
| ------------------------- | ------------------------------------------------------- |
| SQL/迁移/sqlc             | `sqlc generate` 后 `git diff --exit-code`；空库迁移 up  |
| embed/Makefile/Dockerfile | `make build`；启动二进制检查 `/` 与 `/api/v1/not-found` |
| 认证/权限                 | `go test ./... -race`；setup/login/refresh 契约测试     |
| 前端页面                  | `pnpm --dir web test`；关键链路 Playwright              |
| 依赖升级                  | `govulncheck ./...`、`pnpm audit`；单独 commit          |
| 发布                      | `make test`、`make build`、镜像构建                     |

- CI 必须跑完整检查；本地快速检查不能替代 CI。
- 提交前 `git diff --check` 检查空白错误。
- 不在 commit hook 里自动 `git add` 无关文件。

## 8. 分支与合并

- 默认分支 `main` 禁止直接 push；所有改动必须开独立功能分支并通过 PR 合入。
- 合并至 `main` **统一采用 Squash Merge**，将分支内过程提交压缩为一条符合本规范格式的提交（Header 与 PR 标题一致，Body 剔除过程噪音）；同步主干使用 `rebase`，禁止将 `main` merge 回功能分支。
- 完整分支命名、PR 描述模板、压缩提交格式与清理步骤见 [pr.md](./pr.md)。

## 9. Revert 与 Tag

回滚：

```text
revert: feat(auth): 增加 Refresh Token 轮换

This reverts commit 1a2b3c4d5e6f.

Reason: 轮换在并发刷新下误判重放。
```

- 回滚提交必须引用原 commit；不要静默改历史。
- 发布 tag 使用 annotated tag：`vMAJOR.MINOR.PATCH`；tag 必须指向已合并、CI 通过的 commit。
- 预发布：`v0.2.0-rc.1`；不要在 tag 上直接改代码。

## 10. 示例

```text
feat(auth): 增加 Refresh Token 轮换与重放检测

- 轮换时保留上一代哈希用于重放识别
- 同一 session 刷新用行锁串行化，避免多标签页误杀
- 刷新失败统一吊销 token family

Refs: #42
```

```text
fix(users): 邮箱唯一性改为大小写不敏感

使用 lower(email) 唯一索引，入库前统一 trim + lowercase。
```

```text
build(embed): 构建前复制 web/dist 到 internal/webui

Go embed 不能跨 .. 引用前端产物，新增 web-embed 目标。
```

```text
docs(spec): 补充后端 API、安全、数据模型与测试契约
```

```text
feat(api)!: 统一分页响应字段

BREAKING CHANGE: 列表响应新增 page/pageSize，旧客户端需同步升级。
```

## 11. 可选工具

- `commitlint` 校验 type/scope/subject 长度；配置放在仓库根，规则与本节一致。
- `commit-msg` hook 做正则校验；`pre-commit` 只跑快速检查，完整检查交给 CI。
- 签名提交可选；如启用，使用 `git commit -S`，不要伪造签名。
- AI 辅助提交必须人工 review；如配置 co-author trailer，使用真实身份，不添加虚假署名。
