# Go Style Guide

- **格式化**：保存或提交前强制执行 `gofmt -s` / `goimports`。
- **静态检查**：通过 `golangci-lint` 启用 `govet`、`staticcheck`、`errcheck`、`gocritic`、`revive`、`exhaustive`。
- **行宽**：软限制 100–120 字符。不为了凑字数把正常表达式折成碎片；行过长时优先通过拆局部变量或拆函数解决，而不是硬折行。

---

## 1. 命名与包设计

- **包名短小、全小写、单数**：禁止下划线、驼峰，禁止创建无意义的垃圾桶包（`util`、`common`、`helper`、`base`、`misc`）。
- **禁止口吃（Stutter）**：外部调用已经带上包名，导出的符号不要重复包名。
  - Bad：`http.HTTPServer`、`user.UserInfo`、`order.NewOrderService`
  - Good：`http.Server`、`user.Info`、`order.NewService`
- **变量名长度与作用域成正比**：循环和 5 行以内的局部变量用短名（`i`、`ctx`、`req`、`tx`）；结构体字段、包级常量、跨多屏使用的变量用清晰完整的名字。
- **驼峰与缩写一致性**：使用 `MixedCaps`，缩写词保持大小写一致——`userID`、`parseURL`、`httpClient`、`XMLHTTPRequest`（禁止 `UserId`、`ParseUrl`）。
- **Getter 不加 `Get` 前缀**：读字段直接用 `user.Name()`，写字段用 `user.SetName(v)`；只有真正触发耗时计算或远程 IO 时，才考虑 `Fetch` / `Load` / `Compute`。
- **接收者命名**：1–2 个小写字母，反映类型名（如 `func (s *Server)`），同一类型的所有方法必须统一接收者名称，禁止使用 `this`、`self`、`me`。

---

## 2. 接口、结构体与状态管理

- **接收接口，返回具体类型；接口由调用方定义**：不要在实现方提前为每个 Struct 强行配一个 `IXxx` 接口。只有当调用方需要解耦或测试替身时，在调用方定义最小接口（通常 1–3 个方法）。
- **禁止指向接口的指针**：接口本身已包含类型与数据指针，参数和字段永远直接传值 `io.Reader`，不要写 `*io.Reader`。
- **编译期接口实现校验**：如果某类型必须满足特定接口，在文件顶部加编译期断言，避免运行时才暴露签名不匹配：

```go
var _ io.ReadCloser = (*Stream)(nil)
```

- **值接收者 vs. 指针接收者**：
  - 只要类型包含 `sync.Mutex` / 不可拷贝字段、需要修改状态、或结构体较大，一律用指针接收者。
  - 同一类型的方法尽量保持接收者类型一致，不要混用。
- **零值可用，且禁止在导出结构体中嵌入 `sync.Mutex`**：嵌入 Mutex 会把 `Lock()` / `Unlock()` 泄露为外部 API。

```go
// Bad: 外部可直接调用 list.Lock()
type SafeList struct {
    sync.Mutex
    items []string
}

// Good: 锁作为私有字段，零值直接可用
type SafeList struct {
    mu    sync.Mutex
    items []string
}
```

- **初始化结构体必须显式写字段名**：除了极少数固定二元组/坐标，一律使用具名字段初始化，防止后续新增字段破坏顺序或可读性。
- **避免 `init()` 与可变包级全局变量**：依赖一律通过构造函数显式注入。只有纯确定性、无 IO、无外部依赖的静态注册（如编译正则、注册驱动）才允许使用 `init()`。

---

## 3. 函数、控制流与数据边界

- **尽早返回，让主路径贴着左侧（Indent Error Flow）**：把错误和边界条件提前 `return`，成功路径保持最低缩进；`if` 分支里已经 `return` 时，禁止写多余的 `else`。

```go
// Bad
if err != nil {
    return err
} else {
    return process(v)
}

// Good
if err != nil {
    return err
}
return process(v)
```

- **控制函数复杂度与参数数量**：单函数只做一件事，保持紧凑（建议不超过 60 行、参数不超过 4 个）。可选配置项多于 2–3 个时，对外 SDK 用 Functional Options（`WithTimeout(...)`），内部模块优先用配置结构体（`Config` / `Options`），更直白且零样板代码。
- **边界处拷贝 Slice 和 Map**：Slice 和 Map 底层共享引用。接收外部传入的切片/Map 用于长期持有，或向外返回内部状态时，必须拷贝隔离：

```go
// 接收外部数据保存
func (s *Store) SetTags(tags []string) {
    s.tags = slices.Clone(tags)
}

// 暴露内部数据快照
func (s *Store) Snapshot() map[string]int {
    s.mu.Lock()
    defer s.mu.Unlock()
    return maps.Clone(s.counts)
}
```

- **空切片默认用 `nil`，仅在明确需要 JSON `[]` 时用 `[]T{}`**：
  - 内部逻辑声明空切片一律写 `var items []Item`，零内存分配，`len(items) == 0` 行为一致。
  - 如果该切片直接用于 JSON 序列化且接口契约要求返回 `[]` 而非 `null`，显式初始化为 `items := make([]Item, 0, n)` 或 `[]Item{}`。
- **资源释放紧跟获取之后**：打开文件、锁、HTTP Body、数据库连接后，检查完 `err` 立即写 `defer`，不要拖到函数末尾。
- **用 `any` 替代 `interface{}`**，优先使用标准库 `slices`、`maps`、`min`、`max`、`clear`。

---

## 4. 错误处理

- **错误只处理一次（Handle Once）**：要么记录日志并吞掉/降级，要么包装后向上返回——**严禁既打日志又往上 `return err`**，否则一次故障会在调用链每一层重复刷屏。
- **错误字符串格式**：全小写开头（专有名词或缩写除外），结尾**不加标点**，避免带“failed to”叠加成冗长套娃（如 `failed to connect: failed to dial: ...`），直接给上下文即可（`fmt.Errorf("load user %d: %w", id, err)`）。
- **分清 `%w` 和 `%v`**：
  - 调用方需要用 `errors.Is` / `errors.As` 匹配底层错误时，用 `%w`（这属于公共 API 契约的一部分）。
  - 跨架构边界、不希望调用方耦合底层实现细节（如暴露底层 SQL 驱动错误）时，用 `%v` 或转换为本层领域错误。
- **错误匹配一律用 `errors.Is` / `errors.As`**，禁止用 `==` 或类型断言直接比对，防止包装后失效。
- **禁止用 `panic` 做常规错误流**：业务代码和库函数永远返回 `error`。只有程序启动期遇到不可恢复的致命配置错误，或触发了绝对不可能发生的内部逻辑不变式破坏时才考虑 `panic`。
- **`os.Exit` 和 `log.Fatal` 只能出现在 `main()` 中**：其他任何函数调用它们都会跳过所有 `defer` 清理逻辑，且让代码无法单元测试。

---

## 5. 并发与 Context

- **默认写同步函数，把并发决定权留给调用方**：除非 API 本身就是流式/后台服务，否则不要在函数内部偷偷起 Goroutine；调用方需要并发时自己包一层 `go` 或 `errgroup` 即可。
- **`context.Context` 规范**：
  - 始终作为函数第一个参数，命名为 `ctx`。
  - **严禁把 `context.Context` 塞进结构体字段**（生命周期与请求绑定，而非对象绑定）。
  - 不传 `nil`；不确定上下文时传 `context.TODO()`。
- **禁止“发后即忘”（Fire-and-Forget）的 Goroutine**：每起一个 Goroutine，必须同时明确回答两个问题——**它什么时候退出？调用方怎么等它退出？** 优先用 `errgroup.Group` 或 `sync.WaitGroup` + `ctx.Done()` 托管生命周期。
- **Channel 缓冲大小只写 `0` 或 `1`**：无缓冲（`0`）用于同步握手，容量 `1` 用于避免单次信号发送阻塞。任何大于 1 的缓冲大小都必须附带注释说明容量推导依据，防止靠拍脑袋调大缓冲掩盖背压或死锁问题。

---

## 6. 性能与日常微习惯

- **已知容量时预分配**：`make([]T, 0, len(src))`、`make(map[K]V, n)`，避免热路径反复扩容与拷贝。注意切片预分配时长度设 `0`、容量设 `n`，避免 `append` 把新元素追加到一堆零值后面。
- **高频转换用 `strconv` 替代 `fmt`**：`strconv.Itoa(n)` 比 `fmt.Sprintf("%d", n)` 少反射与分配，开销低数倍。
- **循环拼字符串用 `strings.Builder`**，不要在 `for` 循环里用 `+=`。
- **时间表达一律用 `time.Time` 和 `time.Duration`**：禁止在函数签名或结构体里用裸 `int` / `int64` 表示秒数或毫秒（除非是在最外层 DTO/JSON 协议边界，此时字段名必须带明确单位后缀如 `TimeoutMS`）。

---

## 7. 注释与测试

- **注释解释“为什么”，不翻译代码做了什么**：重点写清楚业务边界、并发安全前提（如“调用方必须持有 `mu`”）、反直觉的 workaround 及原因。
- **导出符号文档注释**：以符号名本身开头（`// Client manages ...`），保持完整句子，方便 `go doc` 检索。
- **表驱动测试（Table-Driven Tests）**：多分支逻辑优先用 `[]struct{ name string; ... }` 配合 `t.Run(tc.name, ...)`；公共断言辅助函数第一行必须调用 `t.Helper()`，确保报错行号指向真实用例而不是辅助函数内部。
