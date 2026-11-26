# Server-Driven DataTable

适用于所有服务端分页列表页。目标是将分页、搜索、筛选、排序、列显隐与加载/空态/错误态收敛到泛型表格原语中，禁止每个业务页各写一套。

## 1. 分层职责

```text
src/components/data-table/          # 泛型原语，不 import 任何 feature
├── index.ts                        # 统一导出稳定原语
├── toolbar.tsx                     # 搜索、筛选、重置、列显隐入口
├── pagination.tsx                  # 页码、页大小、首尾页
├── column-header.tsx               # 排序/隐藏表头菜单
├── view-options.tsx                # 列显隐
├── faceted-filter.tsx              # 多选筛选
└── bulk-actions.tsx                # 可选批量操作壳

src/features/<domain>/              # 业务组装层
├── components/<domain>-table.tsx
├── components/<domain>-columns.tsx
└── queries.ts
```

- `components/data-table/*` 只依赖 `Table<TData>`、`ColumnDef<TData>` 等泛型，不绑定任何具体业务字段或接口。
- 列定义、筛选选项、行操作全部留在所属 feature 内；操作列通过局部回调/Provider 触发弹窗，禁止在列定义中直接发起 API 请求或弹全局 toast。

## 2. 分页与查询契约

对齐 [backend/api-contract.md](../backend/api-contract.md) 第 4、7 节，前端手写泛型 DTO：

```ts
export type Paginated<T> = {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
};

export type ListParams = {
  page: number;
  pageSize: number;
  q?: string;
  sort?: string;
  order?: "asc" | "desc";
  [filter: string]: string | string[] | number | undefined;
};
```

- 请求参数固定 `page`（1-based，默认 `1`）、`pageSize`（默认 `20`，选项 `10 / 20 / 50 / 100`）；响应固定返回 `items`（空为 `[]`）、`total`、`page`、`pageSize`。
- 表格行类型必须包含稳定字符串 `id`，供 `getRowId`、行选择与缓存更新使用，禁止使用数组下标作为 key。
- 所有进入请求的参数必须全部进入 TanStack Query key；未使用的 URL 参数不得透传给后端。

## 3. URL 为唯一事实源

每个列表路由通过 Zod `validateSearch` 声明并规范化查询参数：

```ts
const listSearchSchema = z.object({
  page: z.coerce.number().int().min(1).catch(1),
  pageSize: z.coerce.number().int().min(10).max(100).catch(20),
  q: z.string().trim().optional().catch(undefined),
  sort: z.enum(["created_at", "name"]).optional().catch(undefined),
  order: z.enum(["asc", "desc"]).optional().catch(undefined),
  status: z.array(z.string()).optional().catch([]),
});
```

规则：

- 数字参数一律用 `z.coerce.number()`；非法参数用 `.catch()` 回退默认值，避免非法 URL 打崩页面。
- 数组参数序列化规则（重复 key 或逗号分隔）在 Router `parseSearch` / `stringifySearch` 中全局统一，并与后端接口约定一致。
- 默认值（如 `page=1`、`pageSize=20`）可省略不写入 URL；搜索、筛选、页大小变更时必须把 `page` 重置为 `1`。
- 启用排序时，`sort` 与 `order` 必须同步至 URL 并进入 query key，禁止只排当前页。
- 刷新、前进/后退与分享链接必须能恢复同一视图；`useTableUrlState` 必须随 `search` 变化双向同步，不得只在首次挂载读一次。

## 4. TanStack Table 服务端模式

必须启用全量服务端控制，禁止混用客户端分页/筛选/排序模型：

```ts
const total = data?.total ?? 0;
const pageCount = data
  ? Math.max(1, Math.ceil(total / pagination.pageSize))
  : 0;

const table = useReactTable({
  data: data?.items ?? [],
  columns,
  pageCount,
  state: { pagination, columnFilters, sorting, columnVisibility, rowSelection },
  manualPagination: true,
  manualFiltering: true,
  manualSorting: true,
  onPaginationChange,
  onColumnFiltersChange,
  onSortingChange,
  onColumnVisibilityChange: setColumnVisibility,
  onRowSelectionChange: setRowSelection,
  getCoreRowModel: getCoreRowModel(),
  getRowId: (row) => row.id,
});
```

- 不挂载 `getPaginationRowModel` / `getFilteredRowModel` / `getSortedRowModel`。
- 数据总页数缩小（如删除最后一页最后一条）后调用 `ensurePageInRange(pageCount)` 回退至有效页，避免停在空页。
- 行选择仅作用于当前页，批量操作提示须明确“当前页已选 N 项”，不暗示跨页全选。
- 列显隐属于纯 UI 偏好，放组件本地 state 即可，不必写入 URL。

## 5. 列定义、操作栏、行点击与状态呈现

- **列定义**：独立导出 `ColumnDef<T>[]`，不匿名内联；标量字段用 `accessorKey`，操作/派生列用 `id` 且设 `enableSorting: false`、`enableHiding: false`；日期用 `Intl.DateTimeFormat`，状态用语义 `Badge`，长文本用 `LongText`，空值统一显示占位符而非 `undefined`。
- **操作列（Row Actions）**：
  - 固定悬浮在表格最右侧（`sticky end-0`），横向滚动时保持可见，并使用不透明语义背景（通过 `color-mix` 与行背景 / `hover` / `focus-visible` 态保持亮度一致，避免半透明叠加偏差或横向滚动透字），不额外绘制显式竖向分界线；
  - 表头**不展示“操作”可见文字**（表头留空或仅放 `sr-only` 标签）；
  - 单元格内统一渲染 `...`（`MoreHorizontal` / `Ellipsis`）图标按钮（紧凑尺寸如 `size-7`，配合独立高亮态与整行高亮明确区分），点击展开 `DropdownMenu` 展示更多操作；按钮必须提供 `sr-only` / `aria-label`。
- **只读数据行点击查看详情**：
  - 对于只读且具备详情视图的列表，**点击整行直接打开右侧详情抽屉（`Sheet`）**，不再要求必须点击菜单入口；
  - 可点击行必须提供明确视觉与键盘反馈（`cursor-pointer`、hover 高亮，并支持键盘聚焦回车触发）；
  - 行内可交互子元素（选择框、链接、复制按钮、右侧 `...` 操作按钮等）必须阻止事件冒泡（`e.stopPropagation()`），防止误触行点击。
- **Toolbar**：关键词搜索做 300ms 防抖并在清空后移除 `q`；筛选选项来自静态枚举或后端元数据，服务端模式下不展示当前页 `getFacetedUniqueValues()` 计数；“重置”一键清空搜索、筛选、排序并重置 `page=1`；主操作按钮受 `PermissionGuard` 控制。
- **加载态**：首屏渲染 `pageSize` 行 Skeleton 保持列宽稳定；翻页/刷新使用 `placeholderData: keepPreviousData` 保留旧数据并展示轻量 `isFetching` 指示，不整块卸载。
- **空态与错误态**：空数据区分“尚无数据”与“筛选无结果（提供清除筛选）”；查询失败在表格区域内联展示 `Alert` + 重试按钮，不重复刷屏 toast。
- **Query 与 Mutation**：写操作成功后按最小范围 invalidate 列表 query key；删除/批量删除成功后同步清空选择、关闭确认框、提示并修正页码；除有明确回滚设计的乐观状态切换外，不手改本地列表缓存副本。

## 6. 可访问性与反模式

- 保留 `<Table>` 语义，排序表头带 `aria-sort`，选择框与分页/图标按钮带可读 `aria-label` 或 `sr-only`，分页器在窄屏允许换行不撑破视口；横向滚动固定右侧操作列（及必要时的主身份列），不把全部列都设为 sticky。
- **禁止**：服务端分页挂载 `getPaginationRowModel()`；`useEffect` + `useState` 拉取列表；把分页/筛选复制进 Zustand 或多处维护；忽略 `total` 写死页大小；对当前页做筛选却展示“全部结果”；在可点击行中嵌套未阻止冒泡的按钮/链接导致误触详情。
