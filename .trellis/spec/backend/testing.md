# Backend Testing

测试不是“写完补单测”，而是模块验收的一部分。新增/修改模块时，至少补齐本文件对应的层级；没有测试的 PR 不进入合并队列。

## 1. 测试层级

| 层级      | 目标                                   | 工具                                          |
| --------- | -------------------------------------- | --------------------------------------------- |
| 单元      | service 业务规则、错误分支、状态机     | `go test`，fake store/deps                    |
| DB 集成   | SQL、事务、约束、索引、迁移            | 真实 PostgreSQL（testcontainers/Compose）     |
| HTTP 契约 | 路由、状态码、错误体、分页、i18n、权限 | `httptest` + Gin                              |
| E2E       | Setup → 登录 → 业务页 → 登出           | Playwright（前端）                            |
| 架构      | 依赖方向、模块边界                     | `go list` 脚本或 depguard                     |
| 静态/安全 | vet、lint、race、漏洞                  | `go vet`、golangci-lint、`-race`、govulncheck |

## 2. 单元测试

- 优先测 `service.go`：状态校验、权限边界、事务编排、错误码映射。
- `store.go` 的纯映射逻辑可单测；SQL 行为放 DB 集成测试，不要 mock SQL 字符串。
- 表驱动：`[]struct{ name string; ... }` + `t.Run`；断言辅助函数第一行 `t.Helper()`。
- 依赖用最小接口 fake（`deps.go` 的接口），不要为测试引入 mock 框架生成器。
- 时间、UUID、随机数通过注入的 clock/id 生成器控制；不要在测试里 `time.Sleep`。
- 错误断言用 `errors.Is`/`errors.As`；不要只比较字符串。
- 覆盖成功、边界、失败、回滚；权限错误与业务错误分开断言。

## 3. DB 集成测试

- 使用真实 PostgreSQL，不用 SQLite 模拟；`uuid`、`timestamptz`、`jsonb`、`ILIKE`、咨询锁都要真实行为。
- 推荐 testcontainers-go；本地也可用 `docker compose up -d postgres`，但 CI 必须自动化。
- 启动时跑 Goose 迁移到最新；每个测试用独立 schema 或事务回滚；模板数据库可加速。
- 测试必须覆盖：
  - 唯一约束（大小写不敏感邮箱/用户名）；
  - 外键 `ON DELETE` 行为；
  - 事务回滚（跨模块写失败时全部回滚）；
  - Setup 并发只成功一次；
  - 最后一名超管保护在并发下不双空；
  - Refresh Token 轮换/重放检测；
  - 角色权限变更后的缓存失效；
  - 文件 pending/ready 与孤儿清理。
- 测试数据用 factory 生成；不要依赖生产数据或真实 PII。
- 每个测试结束清理数据；不要依赖执行顺序。

## 4. HTTP 契约测试

- 用 `httptest.NewServer` 或 `httptest.NewRecorder` 跑完整中间件链（RequestID、Recovery、Logger、Locale、ErrorHandler、Auth/Permission）。
- 每个端点至少覆盖：
  - 成功状态码与响应结构；
  - `X-Request-Id` 响应头存在且非空；错误体不重复携带 `request_id`；
  - CORS 预检/实际响应暴露 `X-Request-Id`（适用时）；
  - 401（无/坏 token）、403（无权限/禁用）、422（字段校验）、404/409（适用时）；
  - 分页 `items/total/page/pageSize` 与空数组 `[]`；
  - `Accept-Language` 切换后 `message`/`errors[].message` 翻译正确；
  - 权限矩阵：有/无某个权限码的差异。
- 不要断言完整 JSON 字符串；按字段断言，允许新增字段。
- 不要测试 `dbgen` 生成代码本身；测模块 DTO 与 mapper。
- 禁止把 `200` 当错误响应；契约测试要直接抓这种回归。

## 5. E2E

- Playwright 覆盖关键链路：
  1. 空库启动 → `/setup` 初始化 → 进入 Dashboard；
  2. 登录失败/禁用账号/成功登录；
  3. Users 新增、编辑、禁用、删除；
  4. Roles 创建、勾选权限、权限即时生效；
  5. 语言切换（前端文案 + 后端错误/权限名称）；
  6. 文件上传/预览/删除；
  7. 登出后刷新不能恢复登录态。
- E2E 使用独立测试库；每个场景前重置数据，不要依赖上一个场景。
- 选择器优先 role/label；不要用 CSS 层级或文本硬编码（i18n 会变）。
- 等待网络/动画用框架提供的条件，不用固定 `sleep`。
- 失败时保留 trace/screenshot；CI artifact 上传。

## 6. 架构与边界测试

- 用脚本/测试检查 import 方向：
  - `cmd/server` → `internal/app`；
  - `modules/*` 不互相 import；
  - `platform/*` 不 import `modules/*`；
  - `shared/*` 是叶子；
  - `webui` 只依赖标准库。
- 检查 `modules/*/internal/dbgen` 不被模块外引用。
- 检查前端静态资源 `//go:embed` 只出现在 `internal/webui`；全仓 `//go:embed` 仅允许出现在白名单路径（`internal/webui`、内嵌 SQL 迁移、`internal/platform/i18n` 以及 `internal/modules/*/locales.go` 的 `locales/*.toml`）。
- 检查路由是否都挂了鉴权/权限中间件；公开路由必须有显式白名单。
- 这类测试放在 `internal/app` 或 `tools/archtest`，CI 必跑。

## 7. i18n 与文案

- 每个错误码必须在 `zh-CN`/`en-US` 都有译文；缺失直接失败。
- 同一语言内重复 key 启动失败；测试覆盖重复检测。
- validator 字段名映射到 JSON 字段；嵌套字段用点路径。
- 权限码目录的中英文名称/描述完整；权限码本身不翻译。
- 禁止在测试里断言某一条中文文案的完整字符串；断言 key/错误码，或按当前语言取资源。

## 8. 迁移与 sqlc

- CI 从空库跑全部 `up` 迁移；开发环境跑一次 `down`/`up` 验证可逆性。
- `sqlc generate` 后工作区必须干净；CI 跑 `sqlc generate` 后 `git diff --exit-code`。
- 迁移文件只增不改；检查已应用迁移的校验和。
- seed/权限同步测试：重复启动不产生重复行；删除代码中的权限码有明确处理。
- 迁移失败时输出可定位的版本号与 SQL 文件，不吞错。

## 9. 并发与竞态

- CI 至少对核心包跑 `go test -race ./...`。
- 重点覆盖：
  - Setup 并发；
  - 最后一名超管并发禁用/删除；
  - 角色删除与用户分配并发；
  - Refresh Token 并发轮换；
  - 权限缓存失效与事务回滚；
  - 文件上传/删除双写失败。
- 不要用 `time.Sleep` 等待 goroutine；用 channel/WaitGroup/`errgroup` 明确同步。
- 发现 flaky 测试先修根因，不要加 retry 掩盖。

## 10. 静态检查与安全

- `gofmt -s` / `goimports` 必须干净。
- `go vet ./...`、`golangci-lint run` 零错误；启用 `errcheck`、`staticcheck`、`govet`、`gocritic`、`revive`、`exhaustive`。
- `govulncheck ./...` 检查已知漏洞；依赖升级单独提交。
- 测试不得连接生产服务、真实对象存储或真实邮件；用 fake/local 驱动。
- 测试日志/失败信息不得打印密码、token、Cookie、密钥。

## 11. CI 顺序

```bash
gofmt -l .                        # 必须为空
go vet ./...
golangci-lint run
go test -race ./...
sqlc generate && git diff --exit-code
go build ./...
make build                        # 验证 web embed
```

- 前端独立 job：`pnpm install --frozen-lockfile`、`typecheck`、`lint`、`format:check`、`test`、`build`。
- E2E job 依赖 Postgres 服务与前端构建；失败上传 trace。
- 覆盖率不做硬性百分比；核心模块（auth/access/users/audit/files）必须有成功+失败+边界用例。

## 12. 反模式

- mock 数据库约束/事务，然后声称集成测试通过。
- 只测 handler 的 happy path，不测中间件与错误体。
- 用固定 `sleep` 等异步任务。
- 测试之间共享可变全局状态/同一个用户。
- 断言日志文本、SQL 字符串或生成代码。
- 为了覆盖率写无断言的“调用一下”测试。
- 把 E2E 当单元测试跑，导致 CI 又慢又 flaky。
