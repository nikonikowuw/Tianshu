# Frontend Testing & Quality Gates

本规范定义 `web/` 的测试分层、静态检查、包卫生工具与 CI 执行顺序，与 [backend/testing.md](../backend/testing.md) 对称。

## 1. 脚本契约

`web/package.json` 至少提供以下标准脚本：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "typecheck": "tsc -b",
    "lint": "eslint .",
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "test": "vitest run --browser.headless",
    "test:watch": "vitest --browser.headless",
    "test:coverage": "vitest run --coverage --browser.headless",
    "test:e2e": "playwright test",
    "knip": "knip"
  }
}
```

## 2. 测试分层

| 层级              | 工具                                                    | 覆盖目标                                                           |
| ----------------- | ------------------------------------------------------- | ------------------------------------------------------------------ |
| 单元 / 组件测试   | Vitest（browser mode + Playwright Chromium）            | 表单校验、权限守卫、错误归一化、单飞刷新、关键业务组件交互与错误态 |
| 端到端测试（E2E） | Playwright                                              | 真实前后端联调的核心业务闭环                                       |
| 静态与卫生检查    | TypeScript strict、ESLint、Prettier、Knip、`pnpm audit` | 类型安全、代码风格、未使用导出/依赖、安全漏洞                      |

### 单元与组件测试规范

- 测试文件与源码就近同目录放置（`*.test.ts` / `*.test.tsx`）。
- DOM 查询优先语义化选择器（`getByRole`、`getByLabelText`），禁止依赖脆弱的 CSS 层级、硬编码单一语言文本或大范围 snapshot。
- 每个业务 feature 至少覆盖：表单校验与 422 字段回填、核心交互路径、权限显隐与错误/空态分支。
- `src/components/ui/**`、`src/routes/**`、`src/routeTree.gen.ts`、`src/assets/**` 不计入覆盖率考核。
- 认证与权限测试优先通过 `RouterContext` / `AuthGateway` 注入测试替身，不依赖全局可变单例残留。

### E2E 关键链路

涉及初始化、认证、权限、列表 CRUD、语言切换或登出的改动，必须覆盖对应 Playwright E2E（与 [backend/testing.md](../backend/testing.md) 第 5 节对齐）：

1. 空库启动 → `/setup` 初始化 → 进入 Dashboard；
2. 登录失败、禁用账号拦截、正常登录与 `redirect` 回跳；
3. Users / Roles CRUD 及权限变更即时生效；
4. 中英语言切换（前端静态文案 + 后端错误与权限目录）；
5. Files 上传/预览/删除；
6. 登出后刷新无法恢复登录态。

E2E 使用独立测试库并按场景重置数据；等待条件使用框架断言，禁止固定 `sleep`；CI 失败必须上传 trace 与 screenshot artifact。

## 3. 静态检查与工具链规范

- **TypeScript**：开启 `strict`；禁止无理由使用 `any`、`@ts-ignore`、`@ts-expect-error`；`pnpm build` 必须先通过 `tsc -b`。
- **ESLint**：禁止 `console`（开发调试代码不得入库）；启用 `@typescript-eslint/consistent-type-imports`、`no-duplicate-imports`；`src/components/ui/**` 与生成文件可按配置排除。
- **Prettier**：单引号、无分号、80 列、Tailwind class 排序；生成文件加入 `.prettierignore`。
- **Knip**：用于清理死代码、未使用导出与冗余依赖；大幅裁剪或重构后必须运行一次 `pnpm knip`。

## 4. 提交与 CI 顺序

前端独立 CI Job 固定顺序：

```bash
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm format:check
pnpm test
pnpm build
```

整包发布前额外执行根 `make build`，验证 `web/dist` 复制进入 `server/internal/webui/dist` 并完成单二进制嵌入（验收清单见 [backend/architecture.md](../backend/architecture.md) 第 6.4 节）。

## 5. 反模式

- 只跑 `pnpm build` 不跑 `tsc -b` 或 `make build` 就声称可发布。
- 用固定 `setTimeout` / `waitForTimeout` 等待异步请求或弹窗动画。
- 通过 `querySelector('.xxx > div')` 或写死中文文案定位元素，导致 i18n 或样式重构即大面积报错。
- 在测试之间共享全局 store/缓存状态，造成用例互相污染。
- 为了压过报错关闭 ESLint 规则或堆砌 `as any`。
