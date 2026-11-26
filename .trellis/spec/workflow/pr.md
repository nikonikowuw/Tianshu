# Pull Request & Feature Branch Workflow

本规范是 Tianshu 功能分支开发、Pull Request 与 Squash Merge 合并流程的唯一事实源。

配套规范：提交信息格式见 [git-commit.md](./git-commit.md)，评审门禁与双轴检查见 [review.md](./review.md)。

## 1. 端到端开发流程

所有改动必须通过功能分支 + Pull Request 合入 `main`，采用统一 5 步闭环：

1. **从最新 `main` 切出功能分支**
   - 同步主干后创建独立分支，一个分支只对应一个逻辑功能或缺陷修复：
     ```bash
     git fetch origin
     git switch -c feat/<scope>-<slug> origin/main
     ```
   - 分支命名规则：
     - `feat/<scope>-<slug>`（如 `feat/auth-refresh-rotation`）
     - `fix/<scope>-<slug>`（如 `fix/users-email-unique`）
     - `refactor/<scope>-<slug>`、`docs/<slug>`、`build/<slug>`、`chore/<slug>`
2. **在功能分支内开发与自检**
   - 开发过程中允许按检查点小步提交；提交前遵守 [git-commit.md](./git-commit.md) 第 6 节禁止提交清单与 `git diff --check`。
   - 提 PR 前先在本地跑通受影响域的测试与静态检查（见 [backend/testing.md](../backend/testing.md) 与 [frontend/testing.md](../frontend/testing.md)）。
3. **向 `main` 创建 PR**
   - PR 标题**必须严格符合** [git-commit.md](./git-commit.md) 的 Conventional Commits header 格式（`<type>(<scope>): <subject>`），因为它将直接作为最终压缩提交的标题。
   - **不设固定行数上限，按单一可独立验证的逻辑单元切分**：只要服务同一目标且可独立构建、测试与回滚，允许较大体量变更；禁止把无关功能、无关依赖升级或大面积纯格式化塞进同一个 PR。跨层大型需求若能自然拆成可独立交付阶段（如“底座/契约 → 后端模块 → 前端页面与 E2E”），优先自然拆分；强耦合变更允许单 PR 闭环，并在描述中提供评审导读。
   - 按第 2 节模板填写 PR 描述。
4. **基线同步、CI 与 Code Review**
   - 如 `main` 已有新提交，使用 `git rebase origin/main` 同步基线并重跑检查；禁止把 `main` merge 回功能分支。
   - 等待自动化 CI 全绿，并按 [review.md](./review.md) 完成双轴评审、关闭所有 `Blocker`。
5. **强制 Squash Merge 合并并清理分支**
   - 合并至 `main` 时**必须使用 Squash Merge**，将功能分支内的所有过程提交压缩为 `main` 上唯一一条规范提交。
   - 合并前整理压缩提交信息（见第 3 节），剔除开发过程中的零碎提交记录（如 `wip`、`fix typo`、`review fix`）。
   - 合入完成后立即删除远程与本地功能分支，保持分支整洁。

## 2. PR 描述模板

```markdown
## 背景与目标

- 解决什么问题 / 对应需求或 Issue

## 核心改动与取舍

- 关键实现点、边界条件或设计取舍

## 影响范围

- [ ] 数据库迁移 / sqlc（如有，说明是否破坏性变更）
- [ ] API 契约 / 权限码 / 环境变量
- [ ] 文档 / Spec 已同步更新

## 评审导读（大变更必填）

- 核心高风险文件（契约 / 迁移 SQL / 权限安全 / 状态机）：...
- 常规模板与测试文件：...

## 验证结果

- 贴出本地或 CI 关键检查命令及结果

Refs: #<issue-id>
```

## 3. Squash Merge 提交信息契约

合并压缩后，`main` 上的提交信息必须精炼可读，严禁直接保留一长串无意义的过程提交堆叠：

- **Header**：与 PR 标题完全一致，符合 `<type>(<scope>)[!]: <subject>`（不超过 72 字符，祈使句，无句号）。
- **Body**：3–6 行要点总结“做了什么、关键边界/取舍、部署或迁移影响”；删除 `wip` / `lint fix` 等过程噪音。
- **Footer**：必要时保留 `BREAKING CHANGE:`、`Refs:`、`Closes:`。

示例：

```text
feat(auth): 增加 Refresh Token 轮换与重放检测

- 新增 refresh_token_history 表记录任意代际作废哈希
- 同一 session 并发刷新用行锁串行化并保留 30-60s 幂等宽限
- 检测到超宽限重放时吊销整条会话并写入安全审计事件

Closes: #42
```

## 4. 禁止事项（Hard Rules）

- 禁止直接在 `main` 分支开发或 push。
- 禁止使用普通 Merge Commit（`--no-ff`）或未压缩的多条过程提交直接合入 `main`；`main` 历史必须保持线性，且一个 PR 对应一条可独立 `revert` 的原子提交。
- 禁止把 `main` merge 回功能分支制造无意义回合并提交；统一使用 `rebase`。
- 禁止在单个 PR 中混杂无关改动（如业务功能混入无关依赖升级或大面积纯格式化）。
- 禁止在 CI 失败或存在未解决 `Blocker` 时合并 PR。
- 禁止在仓库托管平台开启“允许普通 Merge Commit”作为默认合入方式；建议在仓库设置中仅保留 **Squash and merge** 并开启合并后自动删除分支。
