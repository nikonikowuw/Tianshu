# Architecture

本服务采用单二进制 + 域级垂直切片架构，手动管理依赖与路由。所有业务模块必须严格使用固定目录与文件模板，禁止自行合并文件、省略分层或新增子包。

相关契约：[API](./api-contract.md)、[Security](./security.md)、[Data Model](./data-model.md)、[Testing](./testing.md)。

## 1. 固定目录结构

```text
.
├── cmd/
│   └── server/main.go               # 极薄入口：加载配置、启动 App、处理优雅停机
├── migrations/                      # 全局唯一 DDL 迁移文件（按版本号递增）
├── sqlc.yaml                        # 每个业务模块独立配置一条生成规则
├── Makefile                         # Go 服务侧开发与构建入口：dev/test/lint/sqlc/swagger/web-build/web-embed/server-build/build
└── internal/
    ├── app/                         # 唯一装配中心（唯一允许同时 import platform 与 modules）
    │   ├── app.go                   # 初始化底座、注册各模块 locales、实例化 Service/Handler 与跨模块适配器
    │   └── routes.go                # 统一管理 /api/v1 前缀、鉴权路由组、模块路由挂载与 SPA NoRoute
    ├── webui/                       # 前端静态资源嵌入与 SPA fallback
    │   ├── webui.go                 # //go:embed all:dist + http.Handler
    │   └── dist/                    # 构建期由 web/dist 复制；提交 .gitkeep 占位，不提交构建产物
    ├── platform/                    # 纯技术底座，禁止任何业务逻辑
    │   ├── config/                  # 配置加载与校验
    │   ├── database/                # DB 连接池 + TxManager（通过 ctx 透传事务）
    │   ├── cache/                   # 统一 Cache 接口（默认进程内内存，可选 Redis 切换）
    │   ├── storage/                 # 统一 Storage 接口（LocalFS / s3 驱动）
    │   ├── i18n/                    # 多语言翻译引擎、语言解析与回退策略
    │   ├── httpserver/              # Gin 引擎初始化、全局中间件链、错误收口与 validator 翻译s
    │   └── logger/                  # 结构化日志
    ├── shared/                      # 最底层无业务通用契约（apperr、pagination），禁止 import platform/modules
    └── modules/
        └── <domain>/                # 整个目录为 package <domain>
            ├── queries.sql          # 本模块独占 SQL
            ├── internal/
            │   └── dbgen/           # sqlc 生成代码（禁止手动修改）
            ├── model.go             # 领域模型与状态常量
            ├── errors.go            # 本模块错误码常量（大写模块前缀）与错误构造函数
            ├── locales/             # 本模块多语言文案（zh-CN.toml / en-US.toml）
            ├── locales.go           # //go:embed locales/*.toml，导出 LocalesFS（或 Messages()）供 app 注册
            ├── deps.go              # 仅当本模块依赖其他模块时创建：声明最小私有接口与所需局部结构
            ├── store.go             # 封装 dbgen、从 ctx 自动拾取事务、翻译 DB 错误与行映射
            ├── service.go           # 业务逻辑、状态校验与事务编排（TxManager.WithinTx）
            ├── request.go           # HTTP 请求入参结构体与 binding 校验
            ├── response.go          # HTTP 响应出参结构体与模型转换函数
            ├── handler.go           # 参数绑定、调用 service、c.Error(err) 收口与输出 DTO
            ├── router.go            # 声明本模块相对路径与接口级权限，绑定私有 handler 方法
            └── *_test.go            # 本模块单元测试（与被测文件同目录就近放置）
```

## 2. 强制依赖规则

### 包依赖方向（严格无环）

- `cmd/server` → `internal/app` → `internal/modules/*` & `internal/platform/*` & `internal/shared/*` & `internal/webui`
- `internal/modules/*` → `internal/platform/*` & `internal/shared/*`
- `internal/platform/*` → `internal/shared/*`
- `internal/webui` 为叶子包，只依赖标准库；禁止 import `internal/platform/*` 或 `internal/modules/*`。
- `internal/shared/*` 为最底层叶子包，**禁止** `import` `internal/platform/*` 或 `internal/modules/*`。
- `internal/platform/*` **禁止** `import` 任何 `internal/modules/*`。
- `internal/modules/<A>` **禁止**直接 `import` `internal/modules/<B>`。

### 跨模块协作方式

- 写操作严禁跨表直改，必须通过目标模块的 `Service` 完成；需强一致性时由调用方在 `service.go` 通过 `TxManager.WithinTx` 开启事务并透传 `ctx`。
- 依赖声明固定写在 `<A>/deps.go` 中作为私有接口：
  - 接口方法仅涉及基础类型、`context.Context` 或 `error` 时，在 `internal/app/app.go` 直接注入 `<B>.Service`。
  - 接口方法需要返回结构体数据时，在 `<A>/deps.go` 定义 `<A>` 所需的精简结构体，并在 `internal/app/` 中编写薄适配器完成 `<B>` 模型到 `<A>` 结构的转换。

### 模块内调用链与可见性

- 严格单向调用：`router.go` → `handler.go` → `service.go` → `store.go` → `internal/dbgen`。
- **必须导出**：`Service`、`NewService`、`Handler`、`NewHandler`、`LocalesFS`、`RegisterRoutes`（如需公开接口则另增 `RegisterPublicRoutes`）、供 `app` 适配或测试引用的领域模型、错误码与 `deps.go` 中的消费方窄接口/局部结构（用于 `ServiceDeps` 注入与 `internal/app/adapters.go` 编译期接口断言）。
- **必须私有（小写）**：`store` 结构体及 `newStore`（仅在 `NewService` 内部实例化）、`request.go`/`response.go` 结构体、`handler.go` 中的具体接口方法。

## 3. 运行时收口契约

- **全局中间件固定顺序**：`RequestID` → `Recovery` → `Logger` → `SecurityHeaders` → `CORS`（仅显式启用时）→ `Locale` → `ErrorHandler`；Gin 引擎需按配置设置 `TrustedProxies`，`RateLimit`、`Auth`、`Permission` 按路由组挂载（详见 [security.md](./security.md)）。
- **HTTP 响应契约（无信封）**：
  - 成功响应直接返回 `response.go` 定义的裸 DTO，禁止包裹 `{ code, message, data }` 外壳：查询/更新返 `200`，创建返 `201`，无内容操作返 `204`。
  - 列表分页统一使用 `shared/pagination` 返回 `{ "items": [...], "total": 0, "page": 1, "pageSize": 20 }`；所有响应中的切片字段为空时必须序列化为 `[]`，禁止输出 `null`。
  - 失败响应统一由 `ErrorHandler` 中间件返回真实 HTTP 状态码（`4xx`/`5xx`）与固定 JSON 错误体：`{ "code", "message", "errors"? }`；`errors` 仅在 HTTP `422` 参数校验失败时出现。`request_id` 只通过 `X-Request-Id` 响应头传递，错误体不重复。
- **错误与多语言链路**：
  1. `errors.go` 中所有业务错误码必须带大写模块前缀（如 `ORDER_NOT_FOUND`），对应译文必须成对写入 `locales/zh-CN.toml` 与 `locales/en-US.toml`，由 `locales.go` 通过 `//go:embed locales/*.toml` 暴露并在启动注册时校验 Key 唯一与双语完整性。
  2. `service.go` 只返回 `apperr.Error`，不引入 `gin.Context`，不拼接自然语言提示。
  3. `handler.go` 遇错统一执行 `_ = c.Error(err); return`，不手写 HTTP 状态码分支。
  4. `ErrorHandler` 中间件按 `ctx` 中的语言调用 `platform/i18n` 渲染 `message`，并统一翻译 `validator` 参数校验错误；未预期内部错误仅记录结构化日志，对外固定返回 HTTP `500` 与 `INTERNAL_ERROR`，禁止暴露原始 SQL 或堆栈。
- **事务边界**：仅允许在 `service.go` 调用 `TxManager.WithinTx(ctx, fn)`；`store.go` 通过私有辅助方法从 `ctx` 自动拾取当前事务并绑定 `dbgen.New(tx)`，无事务时回落到连接池。
- **静态资源与 SPA**：前端构建产物由 `internal/webui` 提供；API 路由优先，`NoRoute` 中 `/api/*` 未匹配仍返回 JSON 404，其余路径回退 `index.html`。详见第 6 节。

## 4. 新增业务模块固定清单

新建任意业务模块时，按顺序完整执行以下 6 步，不得跳过：

1. 在 `migrations/` 新增版本化 SQL 迁移文件。
2. 在 `internal/modules/<domain>/` 编写 `queries.sql`，在 `sqlc.yaml` 追加该模块规则并执行 `sqlc generate`。
3. 在 `internal/modules/<domain>/` 按固定模板补齐 `model.go`、`errors.go`、`locales/{zh-CN,en-US}.toml`、`locales.go`、`store.go`、`service.go`、`request.go`、`response.go`、`handler.go`、`router.go`（存在跨模块调用时补 `deps.go`）。显式例外：`setup`、`dashboard` 这类不拥有业务表的编排/只读聚合模块可省略 `queries.sql`、`internal/dbgen/` 与 `store.go`（详见 [data-model.md](./data-model.md) 第 3 节）。
4. 在 `internal/app/app.go` 注册 `<domain>.Messages()` 并实例化 `Service` 与 `Handler`（如需跨模块结构体转换则同步编写适配器）。
5. 在 `internal/app/routes.go` 将模块挂载到对应的公开或鉴权路由组。
6. 编写单测并运行 `go test ./...` 与 linter，验证编译、依赖方向、i18n Key 唯一性与多语言完整性。

## 5. 禁止事项（Hard Rules）

- 禁止把 `dbgen` 结构体直接用于 HTTP 请求绑定或 JSON 响应输出。
- 禁止为成功响应包裹 `{ code, message, data }` 信封，禁止使用 HTTP `200` 返回业务错误。
- 禁止导出 `NewStore` 或跳过 `service.go` 让 `handler.go` 直接调用 `store.go` / `dbgen`。
- 禁止在 `modules/*/router.go` 中硬编码 `/api/v1` 前缀。
- 禁止在任何业务模块中使用 `init()` 注册路由、依赖或文案。
- 禁止在未经人工明确指令的情况下拆分子包、合并固定文件或创建模块分组目录。
- 禁止写 `//go:embed ../web/dist`；embed 只能引用当前包目录内的文件，且业务模块仅允许在 `locales.go` 嵌入 `locales/*.toml`。
- 禁止提交 `internal/webui/dist` 下的构建产物；只提交 `.gitkeep` 占位。
- 禁止让 SPA fallback 吞掉 `/api/*` 的 404；API 未匹配必须保持 JSON 错误响应。

## 6. 前端静态资源嵌入（单二进制）

### 6.1 目录与 embed 约束

前端源码在 `web/`，Go 模块在 `server/`。`//go:embed` 不能跨 `..`，因此不能在 `server` 中直接嵌入 `../web/dist`。构建链必须先把产物复制进包目录：

```text
web/dist  --(server/Makefile: web-embed)-->  server/internal/webui/dist
```

- `server/internal/webui/webui.go` 是唯一前端资源 embed 入口：

```go
//go:embed all:dist
var assets embed.FS
```

- `server/internal/webui/dist/.gitkeep` 必须提交；实际产物被 `.gitignore` 排除。这样干净 clone 下 `go test ./...` / `go build ./...` 仍可编译，不会因为缺少 embed 目录失败。
- `webui.Handler()` 用 `fs.Sub(assets, "dist")` 提供文件；运行时找不到 `index.html` 时返回 `503` 与明确错误页，不能返回目录列表或空 200。
- `internal/webui` 只依赖标准库；不要在这里 import `platform` 或 `modules`。

### 6.2 路由优先级与缓存

Gin 注册顺序固定：

1. `/api/v1/*`、`/healthz`、`/readyz`、开发环境 `/swagger/*`。
2. `r.NoRoute(webuiHandler)` 作为最后兜底。

`NoRoute` 规则：

- `strings.HasPrefix(path, "/api/")`：仍走统一错误收口，返回 JSON `404`，禁止回退 `index.html`。
- 命中真实文件：直接输出；禁止目录列表。
- 无扩展名的前端路径：回退 `index.html`（SPA History Fallback）。
- 带扩展名但文件不存在：返回 `404`，不要回退 `index.html`。
- 非 `GET` / `HEAD`：返回 `405`。

缓存与安全头：

- `index.html`：`Cache-Control: no-cache`（或 `no-store`），确保发版后立即拿到新引用。
- `/assets/*` 等带内容 hash 的静态文件：`Cache-Control: public, max-age=31536000, immutable`。
- 其他静态文件：短缓存（如 `max-age=3600`）。
- 至少设置 `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy`、`Permissions-Policy`；CSP 与 HSTS（TLS 层）由部署层或统一中间件配置，不能放行 `unsafe-eval`。
- Go 标准 `http.FileServer` 默认可能列目录；必须用自定义 handler 或前置检查禁用目录索引。
- 压缩不是首版硬要求；需要时优先在 Nginx/CDN 做 gzip/brotli，或后续增加预压缩资源，不要在 handler 里同步压缩大文件。

### 6.3 构建链

固定命令：

```bash
# 根目录
make build        # 等价于 make -C server build
make test         # Go 测试；不强制构建前端
make dev          # 本地开发入口

# server/（Go 侧快速开发与构建细节）
make dev          # 本地热重载启动 Go 服务（air，不触发前端 embed）
make test         # Go 测试（不触发前端构建）
make lint
make sqlc
make swagger
make web-build    # pnpm --dir ../web install --frozen-lockfile && pnpm --dir ../web build
make web-embed    # 清空 dist（保留 .gitkeep）并复制 ../web/dist
make server-build # go build ./cmd/server
make build        # web-embed + server-build
```

约束：

- `server-build` 必须依赖 `web-embed`；`build` 必须依赖 `server-build`。
- `web-embed` 只清理 `dist` 下的非 `.gitkeep` 文件，再复制 `web/dist/.`；不要删除被跟踪的占位文件。
- `go build` 单独执行不保证前端是最新的；发布/CI 统一走 `make build`。
- `web-build` 使用 `pnpm`，锁文件必须提交；CI 用 `--frozen-lockfile`。
- 发布构建建议 `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"`；是否需要静态链接按目标镜像决定。
- 开发环境：Vite dev server 代理 `/api` 到 Go；Go 可用旧的 embed 产物或直接只提供 API，不要求每次 HMR 都重新 embed。
- 生产 Docker：Node 阶段构建 `web/dist`，复制到 `server/internal/webui/dist`，Go 阶段编译；最终镜像只包含二进制（distroless/static 或 alpine），不包含 Node、pnpm、源码。Docker build context 必须是仓库根（`docker build -f server/Dockerfile .`），否则无法 `COPY web/`。
- `server/Makefile` 负责 Go/embed 细节；根 `Makefile` 只做薄封装，提供 `dev`、`build`、`test`、`lint`、`docker` 等用户入口，避免两套构建逻辑漂移。

### 6.4 发布检查

发布前至少验证：

1. `make build` 成功，`server/internal/webui/dist/index.html` 存在。
2. 启动二进制后 `/` 返回 `index.html`，`/users` 深链接返回 `index.html`。
3. `/api/v1/not-found` 返回 JSON 404，不是 HTML。
4. 发版后 `index.html` 不被强缓存；`/assets/*` 命中长缓存。
5. 镜像中不包含 `web/node_modules`、`web/src`、`server/internal/webui/dist` 之外的源码。
