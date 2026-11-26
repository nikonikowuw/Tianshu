# Journal - ke (Part 1)

> AI development session journal
> Started: 2026-11-25

---

## Session 1: 确立 Code Review 流程规范

<!-- trellis-session: v=3 fp=39b87e9591ed67a1 -->

**Date**: 2026-11-26
**Task**: 确立 Code Review 流程规范
**Branch**: `dev`

### Summary

确立 Tianshu Code Review 规范：个人+AI 协作优先并兼容多人 Approval 门禁、双轴评审（Spec + Standards）与 400 行有效代码软上限、Blocker/Suggestion/Nit 三级意见分级；同步完善 workflow/index.md 与 spec/index.md 索引。
Topics: [.trellis/spec/workflow/review.md](../../spec/workflow/review.md)

### Git Commits

(No associated commits)

## Session 2: 确认 users 采用软删除策略

<!-- trellis-session: v=3 fp=0187c8c13417b19e -->

**Date**: 2026-11-26
**Task**: 确认 users 采用软删除策略
**Branch**: `dev`

### Summary

确认 users 表采用软删除（新增 deleted_at，常规查询默认排除，并显式吊销其全部 user_sessions）；同步更新 backend/data-model.md、backend/index.md、backend/security.md 与 docs/prd/prd.md。唯一索引是否加 WHERE deleted_at IS NULL 暂留待确认。
Topics: [.trellis/spec/backend/data-model.md](../../spec/backend/data-model.md)

### Git Commits

(No associated commits)

## Session 3: 确认 users 部分唯一索引策略

<!-- trellis-session: v=3 fp=ff7c4486129d051a -->

**Date**: 2026-11-26
**Task**: 确认 users 部分唯一索引策略
**Branch**: `dev`

### Summary

确认 users 表采用 WHERE deleted_at IS NULL 部分唯一索引（uq_users_username_lower 与 uq_users_email_lower），软删除后释放用户名与邮箱允许重建；已同步更新 backend/data-model.md、backend/index.md 与 docs/prd/prd.md 并移出待拍板。
Topics: [.trellis/spec/backend/data-model.md](../../spec/backend/data-model.md)

### Git Commits

(No associated commits)

## Session 4: 拍板后端安全基线 5 项待决项

<!-- trellis-session: v=3 fp=48322d682fcbe4cf -->

**Date**: 2026-11-26
**Task**: 拍板后端安全基线 5 项待决项
**Branch**: `dev`

### Summary

全部按推荐拍板 backend/security.md 5 项待决项：Setup 默认 token 模式（支持 local，open 需双保险 allow_open=true）；Refresh Token 重放检测用独立表 refresh_token_history；文件上限通用 10 MiB/头像 2 MiB、禁用 SVG、头像纯 Go re-encode 去 EXIF；不做永久账号锁定而用 cache 15 分钟自动过期软冷却；审计日志默认保留 180 天、MVP 批删不分区。同步更新 security.md、data-model.md、backend/index.md、frontend/architecture.md 与 docs/prd/prd.md。
Topics: [.trellis/spec/backend/security.md](../../spec/backend/security.md)

### Git Commits

(No associated commits)

## Session 5: 拍板最后 3 项架构待决项并清零 Spec 待拍板

<!-- trellis-session: v=3 fp=cba40b7253ca71ff -->

**Date**: 2026-11-26
**Task**: 拍板最后 3 项架构待决项并清零 Spec 待拍板
**Branch**: `dev`

### Summary

拍板最后 3 项架构待决项：模块语言包统一用 locales/{zh-CN,en-US}.toml + locales.go //go:embed 并纳入架构测试白名单；后端模块合并为 access（rbac+role）与 identity（user+profile）；permissions 与系统内置角色用 name_key/description_key 查语言包翻译，自定义角色保留原始 name/description。同步更新 architecture.md、data-model.md、testing.md、backend/index.md、git-commit.md 与 docs/prd/prd.md，全仓 Spec 待拍板项清零。
Topics: [.trellis/spec/backend/architecture.md](../../spec/backend/architecture.md)

### Git Commits

(No associated commits)

## Session 6: 解耦重构前端 Spec 分层与依赖边界

<!-- trellis-session: v=3 fp=56902c869665290a -->

**Date**: 2026-11-26
**Task**: 解耦重构前端 Spec 分层与依赖边界
**Branch**: `dev`

### Summary

按正交关注点重构 .trellis/spec/frontend/ 为 architecture.md、api-auth.md、ui-conventions.md、data-table.md、testing.md 与 index.md；确立 RouterContext 注入 AuthGateway、layout 层不直连 auth-store，去除与后端/PRD 的重复定义；将一次性 shadcn-admin 裁剪清单迁至 .trellis/research/shadcn-admin-bootstrap.md 并删除旧 shadcn-admin.md，同步更新所有跨文档链接。
Topics: [.trellis/spec/frontend/index.md](../../spec/frontend/index.md)

### Git Commits

(No associated commits)

## Session 7: 确立前端样式基线、弹窗抽屉选型与表格交互规范

<!-- trellis-session: v=3 fp=584ad334ab6ab0e5 -->

**Date**: 2026-11-26
**Task**: 确立前端样式基线、弹窗抽屉选型与表格交互规范
**Branch**: `dev`

### Summary

补充 ui-conventions.md 与 data-table.md：组件复用 shadcn-admin 原生样式保持主题统一；明确 Dialog（短表单/确认）vs Sheet（复杂配置/只读详情抽屉）与 ConfirmDialog 选型；表格操作列固定右侧悬浮（sticky end-0）、无可见表头标题、单元格显示 ... 更多菜单；只读且有详情的列表支持点击整行直接打开详情抽屉并做好子元素防冒泡。
Topics: [.trellis/spec/frontend/data-table.md](../../spec/frontend/data-table.md)

### Git Commits

(No associated commits)


## Session 8: 确立功能分支 PR 与强制 Squash Merge 规范
<!-- trellis-session: v=3 fp=014c3026cb4194bd -->

**Date**: 2026-11-26
**Task**: 确立功能分支 PR 与强制 Squash Merge 规范
**Branch**: `dev`

### Summary

新增 .trellis/spec/workflow/pr.md，确立功能分支开发 -> 开 PR -> rebase 同步 + CI/Review -> 强制 Squash Merge 压缩提交并清理分支的端到端开发流，附 PR 描述模板与压缩提交契约；同步更新 workflow/git-commit.md、workflow/review.md、workflow/index.md 与 spec/index.md。
Topics: [.trellis/spec/workflow/pr.md](../../spec/workflow/pr.md)

### Git Commits

(No associated commits)


## Session 9: 修正 PR 规模边界：取消 400 行上限，改按单一逻辑单元与导读控制
<!-- trellis-session: v=3 fp=71c08fabbd825272 -->

**Date**: 2026-11-26
**Task**: 修正 PR 规模边界：取消 400 行上限，改按单一逻辑单元与导读控制
**Branch**: `dev`

### Summary

修正 workflow/pr.md 与 workflow/review.md 中的 PR 规模规则：取消原先 400 行有效代码软上限以适配 Agent 辅助开发，改为按单一可独立验证的逻辑单元切分（禁止混杂无关改动），跨层大型需求优先自然分层，单 PR 大变更须提供评审导读（区分核心高风险文件与常规模板/测试文件）并由 CI 与 AI 初审兜底。
Topics: [.trellis/spec/workflow/review.md](../../spec/workflow/review.md)

### Git Commits

(No associated commits)


## Session 10: 确立最多 3 轮的循环 Review-Fix 机制
<!-- trellis-session: v=3 fp=7d8adcb0a34c8ee9 -->

**Date**: 2026-11-26
**Task**: 确立最多 3 轮的循环 Review-Fix 机制
**Branch**: `dev`

### Summary

更新 workflow/review.md 与 workflow/index.md：评审采用循环 review-fix 流程且最多不超过 3 轮；第 1 轮全量评审所有变更，第 2、3 轮定向复检上一轮提出的问题及修复改动，超 3 轮仍未收敛则暂停循环转人工重新审视方案。
Topics: [.trellis/spec/workflow/review.md](../../spec/workflow/review.md)

### Git Commits

(No associated commits)


## Session 11: RBAC 权限码采用标准动作细粒度拆分
<!-- trellis-session: v=3 fp=8ae445c79878daf9 -->

**Date**: 2026-11-26
**Task**: RBAC 权限码采用标准动作细粒度拆分
**Branch**: `feat/access-core`

### Summary

用户确认 RBAC 权限码采用标准动作全拆（13 项：dashboard:read、users:read/create/update/delete、roles:read/create/update/delete、audit:read、files:read/upload/delete），不将增删改合并为单一 write；已同步至 access 模块权限目录、双语词条、docs/prd/prd.md 与 .trellis/spec/backend/data-model.md。
Topics: [.trellis/spec/backend/data-model.md](../../spec/backend/data-model.md), [docs/prd/prd.md](../../../docs/prd/prd.md)

### Git Commits

(No associated commits)


## Session 12: 调整配置加载顺序、极简 Setup + 环境变量初始化与 8 位密码基线
<!-- trellis-session: v=3 fp=876f255870b36bb5 -->

**Date**: 2026-11-26
**Task**: 调整配置加载顺序、极简 Setup + 环境变量初始化与 8 位密码基线
**Branch**: `fix/dev-ergonomics`

### Summary

用户确认三项开发体验优化：1) 配置加载支持 -config / TIANSHU_CONFIG 严格加载，默认按 configs/config.toml -> config.toml -> configs/config.local.toml -> config.local.toml 叠加，config.example.toml 仅作模板；2) 取消繁琐的 Setup Token 机制，/setup 极简填表并新增 TIANSHU_ADMIN_* 环境变量启动期无人值守自动建超管；3) 密码最小长度由 12 位调整为 8 位（8-128 字符，遵循 NIST SP 800-63B）。
Topics: [.trellis/spec/backend/security.md](../../spec/backend/security.md), [docs/prd/prd.md](../../../docs/prd/prd.md)

### Git Commits

(No associated commits)


## Session 13: 确立 AI 评审强制独立 Subagent 上下文（写审分离）规范
<!-- trellis-session: v=3 fp=75f47c200cf5726a -->

**Date**: 2026-11-26
**Task**: 确立 AI 评审强制独立 Subagent 上下文（写审分离）规范
**Branch**: `feat/users-management`

### Summary

确认 AI 代码评审强制采用写审分离：主 Agent 负责自动化前置门禁与修复编排，实质双轴评审委托给独立上下文的只读 Reviewer Subagent，第 2/3 轮定向复检复用该独立评审会话；同步更新 tianshu-review skill。
Topics: [.trellis/spec/workflow/review.md](../../spec/workflow/review.md)

### Git Commits

(No associated commits)
