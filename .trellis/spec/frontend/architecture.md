# Frontend Architecture

本规范约束 `web/` 的目录分层、单向依赖、状态归属与前端构建输出边界。具体业务页面需求归属 [docs/prd/prd.md](../../../docs/prd/prd.md)，不写入本架构契约。

相关契约：[API & Auth](./api-auth.md)、[UI Conventions](./ui-conventions.md)、[DataTable](./data-table.md)、[Testing](./testing.md)、[Backend Architecture](../backend/architecture.md)。

## 1. 分层与目录结构

```text
web/
├── src/
│   ├── app/                     # 应用装配：Provider 树、Router/QueryClient 实例化、依赖注入
│   ├── routes/                  # TanStack Router 文件式路由声明（只做壳，不写业务逻辑）
│   ├── features/                # 按业务域纵向切分的独立模块
│   │   └── <domain>/
│   │       ├── index.tsx        # 页面入口（供 routes 引用）
│   │       ├── api.ts           # 本域接口调用函数
│   │       ├── queries.ts       # queryOptions / mutation hooks
│   │       ├── types.ts         # 手写 DTO（对齐 backend/api-contract.md）
│   │       ├── schema.ts        # 表单与路由搜索参数校验（按需）
│   │       └── components/      # 本域私有表格、列定义、弹窗与局部状态
│   ├── components/
│   │   ├── ui/                  # shadcn 生成原语层（零业务逻辑）
│   │   ├── data-table/          # 泛型服务端表格原语（不绑定具体业务 DTO）
│   │   ├── layout/              # App Shell（纯展示/布局壳，通过 props/slot 接收导航与用户信息）
│   │   └── shared/              # 跨 feature 共享业务无关组件（ConfirmDialog、PermissionGuard 等）
│   ├── lib/                     # 基础设施：api client、auth/permission 抽象、i18n、通用工具
│   ├── stores/                  # 极薄客户端全局状态（仅内存会话与少量 UI 偏好）
│   ├── hooks/                   # 跨 feature 通用 React hooks
│   ├── i18n/                    # i18next 初始化与 locales/{zh-CN,en-US}/
│   ├── styles/                  # Tailwind CSS v4 入口与语义主题变量
│   ├── main.tsx                 # 极薄挂载入口
│   └── routeTree.gen.ts         # TanStack Router 生成文件（禁止手改）
├── index.html
├── components.json
├── package.json
├── pnpm-lock.yaml
└── vite.config.ts
```

## 2. 强制依赖方向（严格无环）

- `main.tsx` → `app/` → `routes/` & `lib/` & `stores/` & `components/layout/`
- `routes/*` → `features/*` & `app/`（仅通过 `RouterContext` 消费能力）
- `features/<A>` → `components/*`、`hooks/*`、`lib/*`；**禁止** `features/<A>` 直接 import `features/<B>`
- `components/layout/*`、`components/data-table/*`、`components/shared/*` → `components/ui/*`、`hooks/*`、`lib/utils`；**禁止**反向 import `features/*` 或直接耦合 `stores/auth-store`
- `components/ui/*` 为最底层叶子组件，**禁止** import `features/*`、`stores/*`、`lib/api/*`

解耦约束：

- **Layout 与认证解耦**：`AppSidebar`、`Header`、`NavUser`、`CommandMenu` 等布局组件通过 props 或装配层 adapter（在 `app/` 或 `_authenticated` 布局路由中接线）接收用户资料、过滤后的导航项与登出回调，不直接读 `auth-store`。
- **跨 feature 共享**：复用 UI 抽到 `components/shared/`，复用逻辑抽到 `hooks/` 或 `lib/`，跨域数据通过 API 获取，严禁横向引用。
- **路由文件极薄**：`src/routes/*` 只保留 `validateSearch`、`beforeLoad`、`component`、`staticData`，不写请求逻辑或大段 JSX。

## 3. RouterContext 与路由守卫

通过 `RouterContext` 显式注入依赖，路由守卫不得直接依赖全局单例 store：

```ts
export type AuthGateway = {
  getStatus: () => "unknown" | "authenticated" | "unauthenticated";
  ensureSession: () => Promise<boolean>;
  hasPermission: (code: string) => boolean;
};

export type RouterContext = {
  queryClient: QueryClient;
  auth: AuthGateway;
};
```

路由组织与守卫规则：

- 使用 pathless group 区分访问域：`(setup)`、`(auth)`、`(errors)` 与受保护布局 `_authenticated`。
- 守卫执行顺序固定：**初始化状态（Setup）→ 登录会话（Session）→ 页面权限（Permission）**：
  1. `_authenticated` 布局路由的 `beforeLoad` 检查 Setup 状态并调用 `context.auth.ensureSession()`；
  2. 具体子路由的 `beforeLoad` 调用 `context.auth.hasPermission(code)`，无权限重定向至 `/403`；
  3. `(setup)` 与 `(auth)` 路由反向拦截：已初始化时离开 `/setup`，未初始化时从 `/sign-in` 跳 `/setup`，已登录时离开 `/sign-in`。
- 会话处于 `unknown` 时渲染全屏骨架屏，避免刷新闪现登录页。
- `redirect` 回跳参数必须校验为站内相对路径（以 `/` 开头、不以 `//` 开头、不含协议 `://`），非法值回退 `/`。
- 页面级搜索参数一律用 Zod `validateSearch` 声明并配 `.catch()` 默认值；数字参数使用 `z.coerce.number()`；feature 组件通过 `getRouteApi(routeId)` 读取类型化参数。

## 4. 状态归属矩阵

| 状态类型                             | 唯一归属                                        | 持久化           |
| ------------------------------------ | ----------------------------------------------- | ---------------- |
| Access Token、当前用户、角色与权限码 | `stores/auth-store.ts`（经 `AuthGateway` 暴露） | 仅内存           |
| 服务端列表、详情、聚合数据           | TanStack Query                                  | Query Cache 内存 |
| 分页、搜索、筛选、排序               | TanStack Router search params                   | URL              |
| 主题、侧边栏布局偏好                 | UI Context / Cookie                             | Cookie           |
| 界面语言                             | i18next                                         | `localStorage`   |
| 弹窗开关、当前编辑行                 | feature 内部局部 Context / state                | 组件内存         |
| 表单草稿                             | react-hook-form                                 | 组件内存         |

禁止把服务端数据复制进 Zustand，禁止在多个组件各自维护同一份分页/筛选状态，禁止用 `useEffect` + `useState` 替代 TanStack Query。

## 5. Provider 装配顺序

在 `src/app/` 统一装配，固定层级：

```text
QueryClientProvider
  I18nextProvider
    ThemeProvider
      RouterProvider
```

- `Toaster` 与 `NavigationProgress` 挂在根路由；
- TanStack Query/Router Devtools 仅在 `import.meta.env.DEV` 挂载；
- 根路由配置 `notFoundComponent` 与 `errorComponent`；
- Vite SPA 不使用 `'use client'` 指令。

## 6. 前端构建与联调输出契约

前端只负责自身构建产出与开发联调边界；单二进制 `//go:embed` 复制链、Go `NoRoute` SPA fallback 与缓存安全头以 [backend/architecture.md](../backend/architecture.md) 第 6 节为唯一事实源，本文不重复定义：

- `vite.config.ts` 保持 `base: '/'`、构建输出目录 `web/dist`。
- 开发环境由 Vite dev server（默认 5173）代理 `/api` 至 Go 后端（默认 8080），HMR 不经过 Go embed。
- `VITE_API_BASE_URL` 默认 `/api/v1`；任何前端环境变量不得存放密钥。
- `pnpm build` 只负责产出 `web/dist`；发布与整包验证统一走根 `make build`。
