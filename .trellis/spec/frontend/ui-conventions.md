# Frontend UI, Forms & i18n Conventions

本规范约束组件分层、Tailwind CSS v4 样式、表单与弹窗/抽屉交互以及国际化。

`shadcn-admin` 上游基线、一次性裁剪清单与升级步骤见 [shadcn-admin-bootstrap.md](../../research/shadcn-admin-bootstrap.md)，不混入本长期契约。

## 1. 组件四层边界与命名

| 层     | 路径                           | 规则                                                                                                                                                  |
| ------ | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| 原语层 | `src/components/ui/**`         | shadcn 生成组件；零业务逻辑；仅允许样式/可访问性/上游定制                                                                                             |
| 共享层 | `src/components/shared/**`     | 跨 feature 复用组件（`ConfirmDialog`、`PasswordInput`、`LongText`、`PermissionGuard` 等）；不 import `features/*`                                     |
| 布局层 | `src/components/layout/**`     | App Shell（`Header`、`Main`、`AppSidebar`、`NavUser`、`CommandMenu`）；通过 props/slot 接收导航与用户信息，不直接 import `auth-store` 或 `features/*` |
| 表格层 | `src/components/data-table/**` | 泛型服务端表格原语（见 [data-table.md](./data-table.md)）；不绑定业务 DTO                                                                             |
| 业务层 | `src/features/<domain>/**`     | 页面、列定义、弹窗、feature API；禁止横向 import 其他 feature                                                                                         |

编码与命名约定：

- 文件名使用 `kebab-case`，导出组件使用 `PascalCase`，自定义 hook 以 `use-` 文件名 + `useXxx` 导出；一文件一主组件，默认使用命名导出（路由文件导出 `Route`）。
- Props 优先扩展原生类型（`React.ComponentProps<'button'>`）；React 19 直接接收 `ref` prop，不新增 `forwardRef`。
- 条件类名统一用 `cn()`；通用图标统一用 `lucide-react`，品牌图标放 `src/assets/brand-icons`，不手写通用 inline SVG。
- 禁止新建 `utils`、`common`、`helpers` 垃圾桶文件，按职责归位到具体模块。

## 2. 样式与 Tailwind CSS v4

- **视觉风格基线**：优先复用 `shadcn-admin` 原生组件样式、变体、圆角、边框与间距节奏，保持整站主题风格统一，不另起炉灶自造第二套视觉语言。
- 采用 Tailwind CSS v4 CSS-first 配置（无 `tailwind.config.js`）；主题 token 定义在 `src/styles/theme.css`，入口为 `src/styles/index.css`。
- `components.json` 固定基线：`style: new-york`、`baseColor: slate`、`cssVariables: true`、`iconLibrary: lucide`，不擅自变更。
- 严格使用语义 token（`bg-background`、`text-foreground`、`text-muted-foreground`、`border-border`、`bg-primary`、`bg-destructive`），禁止在 feature 中硬编码色阶（如 `bg-slate-100`、`text-gray-500`）。
- 优先使用 `gap-*` 与逻辑属性（`ms-*`、`me-*`、`ps-*`、`pe-*`、`text-start`、`text-end`），正方形使用 `size-*`，响应式遵循 mobile-first。
- 暗色模式通过 `<html class="dark">` 切换；新增组件必须同时验证 light/dark 对比度。
- 动画优先使用 `tw-animate-css`，自定义动画尊重 `prefers-reduced-motion`。
- 字体使用本地/系统字体与 CJK fallback，不依赖运行时外链字体；不引入 CSS Modules 或 CSS-in-JS。

## 3. 布局与页面壳

- 受保护页面统一复用 `AuthenticatedLayout`（`SidebarProvider` + `AppSidebar` + `SidebarInset`），页面内部保持 `Header` + `Main` 结构；`Main` 支持 `fixed`（设置类窄页）与 `fluid`（宽表格页）。
- 侧边栏与 `Cmd/Ctrl+K` 命令面板共享同一份强类型导航配置，并由装配层按当前用户权限与 i18n 翻译后传入。
- 可访问性底线：保留 `SkipToMain`；纯图标按钮必须带 `aria-label` 或 `sr-only`；弹窗关闭后焦点返回触发元素；页面保留 401/403/404/500 错误页。

## 4. 表单与弹窗

表单约定：

- 统一 `react-hook-form` + `zodResolver` + shadcn `Form`（`FormField` / `FormItem` / `FormLabel` / `FormControl` / `FormMessage`），不手写零散 label/error。
- Zod schema 放在组件旁或 feature 的 `schema.ts`；校验错误文案通过工厂函数或 `useMemo` 绑定当前 `t`（取自 `validation` namespace），禁止模块顶层硬编码语言文本。
- 密码输入统一用 `PasswordInput`，正确标注 `autoComplete`（登录 `current-password`，新建/改密 `new-password`）。
- 提交期间按钮禁用并显示加载状态，防止重复提交；服务端 `422` 映射回 `form.setError`，无字段错误时在表单顶部展示错误提示。

弹窗、侧边抽屉与提示约定：

- **容器选型（`Dialog` vs `Sheet`）**：
  - `Dialog`（居中模态弹窗）：用于短平快表单（字段少、无需对照背景列表，如简单新建/编辑、改密、重命名）与操作确认；
  - `Sheet`（右侧侧边抽屉）：用于字段较多的复杂配置/编辑表单（如权限矩阵）及只读详情视图（长文本、结构化 JSON、元数据展示），便于纵向滚动与保留列表上下文；
  - `ConfirmDialog`：专用于删除、禁用、踢出会话等破坏性二次确认，明确传入 `destructive` 与 `confirmText`，异步提交期间禁用关闭与重复点击，默认焦点不落在危险按钮上。
- 简单开关用 `useDialogState<T>()`；同 feature 多弹窗/抽屉使用局部 Provider 管理 `open` 与 `currentRow`。
- 关闭弹窗或抽屉时同步执行 `form.reset()` 与清空 `currentRow`，不用 `setTimeout` 延迟清理。
- 轻提示统一用 `sonner`；禁止 `alert` 与 `console.log`。

## 5. 国际化（i18n）

- 使用 `i18next` + `react-i18next`，支持 `zh-CN`（默认）与 `en-US`；语言解析顺序：持久化偏好 → `navigator.language` → `zh-CN`。
- 语言资源按 namespace 拆分（`common`、`errors`、`validation` 及各业务域 namespace）；组件内禁止硬编码可见文案。
- 权限码、错误码、API 路径、技术标识不翻译；后端已按 `Accept-Language` 翻译的 `message` 直接展示，不做二次翻译。
- 切换语言时同步更新 `document.documentElement.lang`、持久化偏好，并失效依赖后端本地化文案的 TanStack Query 缓存。
- 日期与数字格式化统一用 `Intl.DateTimeFormat` / `Intl.NumberFormat`（传入当前 locale），禁止手工字符串拼接。
