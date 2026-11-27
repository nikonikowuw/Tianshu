# Frontend Spec

适用范围：`web/`（Vite + React 19 + TypeScript + TanStack Router/Query/Table + Tailwind CSS v4 + shadcn/ui + Zustand）。

当前已完成 `web/` 基础框架底座、`/setup` 首次初始化向导、`/sign-in` 完整登录鉴权闭环（含路由守卫、内存 Access Token、单飞静默刷新与跨标签登出）、`/` 仪表盘概览页（实时 KPI 统计卡片、Recharts 7/14/30 天活动趋势图、近期审计动态与按权限控制的快捷入口）、`/users` 用户管理页、`/roles` 角色与权限矩阵页、`/menus` 动态菜单树管理页、`/audit-logs` 审计日志只读检索页、`/files` 文件上传与存储管理页（拖拽队列上传、服务端驱动 `DataTable`、图片短时签名预览 `Sheet` 与删除确认）以及 `/settings` 个人中心与系统设置（个人资料与头像上传/移除、改密与他端下线、活跃设备会话管理、外观/布局/双语偏好）。具体页面字段与业务功能归属 [docs/prd/prd.md](../../../docs/prd/prd.md)；后端 HTTP、安全与单二进制 embed 契约见 [backend/index.md](../backend/index.md)。

| 文档                                     | 内容                                                                       | 何时读                              |
| ---------------------------------------- | -------------------------------------------------------------------------- | ----------------------------------- |
| [architecture.md](./architecture.md)     | 目录分层、单向依赖、状态归属、RouterContext 注入、前端构建输出契约         | 搭骨架、改分层、加新 feature 或路由 |
| [api-auth.md](./api-auth.md)             | HTTP 客户端边界、错误归一化、TanStack Query、会话刷新、权限抽象、文件访问  | 接接口、改认证/权限/文件预览        |
| [ui-conventions.md](./ui-conventions.md) | 组件四层边界、样式 token、表单/弹窗/抽屉、i18n                             | 写组件/表单、调样式、改文案         |
| [data-table.md](./data-table.md)         | 服务端驱动 DataTable、URL 状态、分页/筛选/排序、空态/错误态与可访问性      | 开发任意服务端分页列表页            |
| [testing.md](./testing.md)               | 脚本清单、Vitest browser mode、Playwright E2E、Lint/Format/Knip 与 CI 顺序 | 写测试、接 CI、提交前自检           |

上游 `shadcn-admin` 基线、一次性裁剪与升级清单见 [shadcn-admin-bootstrap.md](../../research/shadcn-admin-bootstrap.md)。

## 不可协商

- Access Token 仅存内存状态；禁止写入 `localStorage`、`sessionStorage` 或 JS 可读 Cookie。
- 服务端状态一律走 TanStack Query；禁止组件内用 `useEffect` 手写 fetch 或绕过统一 HTTP 客户端。
- 严禁跨 feature 直接 import；`components/ui/**` 零业务逻辑；`components/layout/**` 不直连认证 store。
- 所有可见文案走 i18n；权限码、错误码、API 路径不翻译。
- `src/routeTree.gen.ts` 为生成文件，禁止手改。
- 不使用 OpenAPI 双向代码生成；前端 DTO 手写并与 [backend/api-contract.md](../backend/api-contract.md) 对齐。
