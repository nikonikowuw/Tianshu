# shadcn-admin Bootstrap & Upgrade Notes

本笔记记录 Tianshu 基于 `satnaing/shadcn-admin` 的上游基线、一次性裁剪与改造清单及上游升级约束；长期生效的组件、样式与交互契约归属 [ui-conventions.md](../spec/frontend/ui-conventions.md)。

## 1. 上游基线

- 仓库：[satnaing/shadcn-admin](https://github.com/satnaing/shadcn-admin) **v2.2.1**
- Commit：[`a156d9b`](https://github.com/satnaing/shadcn-admin/commit/a156d9b90519348c4d71f0f0e6fcea179c49a43f)（2026-11-25）
- 依赖基线：React 19、Vite 8、TypeScript 6、Tailwind CSS v4、TanStack Router 1.x、TanStack Query 5.x、TanStack Table 8.x、Zod 4、Zustand 5、react-hook-form 7、axios 1、sonner 2、Recharts 3。
- 原则：上游自述是 UI 集合而非完整 starter，仅继承 UI 壳、布局、表格原语与工程配置，不继承 mock 认证与业务假数据；保留 MIT License 与 attribution。

## 2. 保留清单

- `src/components/ui/**`：shadcn 生成组件层。
- `src/components/layout/{header,main,app-sidebar,nav-group,nav-user,authenticated-layout,types}`：App Shell。
- `src/components/data-table/**`：通用表格原语。
- `src/components/{confirm-dialog,password-input,long-text,select-dropdown,navigation-progress,skip-to-main,theme-switch,search,command-menu,profile-dropdown,sign-out-dialog}`（按新架构归入 `shared/` 或 `layout/`）。
- `src/context/{theme-provider,layout-provider,search-provider}`。
- `src/features/{auth/sign-in,dashboard,users,settings,errors}` 的页面布局骨架。
- `src/hooks/{use-dialog-state,use-mobile,use-table-url-state}`。
- `src/lib/{utils,cookies,handle-server-error}`（按 Tianshu 契约改造）。
- `src/styles/**`、`components.json`、`vite.config.ts`、`tsconfig*.json`、ESLint/Prettier/Knip 配置。

## 3. 删除清单

- **Clerk 相关**：`@clerk/react`、`src/routes/clerk/**`、`src/assets/clerk-*`、`ClerkLogo` 及相关 sidebar/route。
- **认证 Demo**：`sign-up`、`sign-in-2`、`forgot-password`、`otp`、社交登录按钮。
- **业务 Demo**：Tasks、Chats、Apps、Help Center、Sponsors、Upgrade/Billing/Notifications/New Team。
- **Mock 数据**：`@faker-js/faker`、`features/users/data/users.ts`、`lib/show-submitted-data.tsx`、所有假统计卡与假图表。
- **其他残留**：`'use client'` 指令、无用截图、上游品牌文案、`use-intl` 相关 import 排序项。
- **可选移除**：`DirectionProvider`、`FontProvider`、RTL 切换与 RTL 专用样式（Tianshu 仅支持中/英 LTR，新代码保留逻辑属性写法）。

## 4. 改造清单

1. `stores/auth-store.ts`：改为纯内存 Access Token + 真实 `/auth/me`，通过 `AuthGateway` 暴露给 `RouterContext`；禁止 Cookie 存 Token。
2. `lib/cookies.ts`：仅服务主题、布局、侧边栏状态，不得用于认证。
3. `lib/handle-server-error.ts`：改消费 `ApiError`（支持 `requestId` 与后端已翻译 `message`）。
4. `app/` 与 `main.tsx`：接入 i18n、真实 `QueryClient` 与 `RouterContext` 装配；移除 Clerk/mock 逻辑。
5. `components/layout/**`：改造成 props/slot 注入（接收过滤后的导航与用户信息，不直连 `auth-store`）；`sidebar-data` 改为 Tianshu 菜单并带 `permission` 与 i18n key。
6. `features/dashboard`：接入真实 `GET /dashboard/summary`，移除假 Tabs 与假数据。
7. `features/users`：改造为服务端分页/筛选/排序与真实 CRUD。
8. 新增 `features/{setup,roles,audit-logs,files}` 与 `lib/{api,auth,permissions}`。
9. 裁剪完成后执行 `pnpm knip` 清理残留依赖与死导出。

## 5. 上游升级边界与检查清单

硬性约束：

- 保留上游 MIT License 与 attribution，并在 `web/package.json` 中记录当前基线版本。
- 禁止直接 `git pull` 覆盖 `src/components/*` 或 `src/features/*`。
- shadcn CLI 仅用于新增/更新标准 `src/components/ui` 组件，不直接覆盖已定制文件。

升级 `shadcn-admin` 前逐项核对：

1. 记录目标 commit/tag 与当前基线的 diff。
2. 逐个手工合并定制过的 `src/components/ui` 文件（通用：`badge`、`scroll-area`、`sonner`、`separator`；逻辑属性：`alert-dialog`、`calendar`、`command`、`dialog`、`dropdown-menu`、`select`、`table`、`sheet`、`sidebar`、`switch`）。
3. 核对 `components.json`、Tailwind v4 主题变量与 `vite.config.ts` 路由插件变化。
4. 核对 TanStack Router / Query / Table 破坏性变更。
5. 重跑 `pnpm typecheck`、`pnpm lint`、`pnpm test`、`pnpm build` 并检查 light/dark、移动端侧边栏、DataTable 分页、命令面板与错误页。
6. 更新 `web/package.json` 的基线记录与 README。
