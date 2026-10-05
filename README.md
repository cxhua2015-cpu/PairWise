# batchqueue

并发安全的内存型批处理优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、插入与删除。
- 不维护持久堆；`Pop`/`Snapshot` 时按需收集候选并用 `slices.SortFunc` 排序，
  规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。revision 单调递增、不复用，
  因此不参与排序键。
- 单个 `sync.Mutex` 保护全部内部状态（`now`、`generation`、`nextRevision`、`items`），
  所有公开方法可并发调用。

## 候选事务

`Apply` 采用候选事务（candidate transaction）：

1. 校验 `Now`（非负、单调，否则 `ErrInvalidInput`/`ErrTime`）。
2. 对整批 ops 做完整结构校验（kind 合法、ID 字符集与字节上限、
   Cancel 不得携带 Priority/ReadyAt 等额外字段），任一失败返回 `ErrInvalidInput`，
   此阶段不读取队列状态。
3. 用 `maps.Clone` 克隆 `items`，在克隆上顺序执行 Enqueue（分配 revision，
   重复 ID 报 `ErrExists`）/ Cancel（缺失报 `ErrNotFound`）。
4. 仅在末尾做最终容量检查，超限报 `ErrCapacity`。
5. 任一步失败直接丢弃克隆——时间、状态、revision 天然回滚；成功才提交，
   非空批次 generation 恰好加一，空批次不变。

## 所有权

- `Pop` 与 `Snapshot` 返回的切片及其元素均为新建副本，调用方可自由修改，
  不影响队列内部状态；队列后续变更也不会反映到已返回的切片。
- `Item` 为纯值类型，无指针/切片字段，拷贝即深拷贝。

## 复杂度

设批次长度 `b`、队列大小 `n`、就绪任务数 `r`、弹出上限 `k`：

- `New`：O(1)。
- `Apply`：结构校验 O(b·L)（L 为 ID 长度）；克隆 O(n)；执行 O(b)；总 O(n + b·L)。
- `Pop`：收集 O(n)，排序 O(r log r)，删除 O(k)；总 O(n + r log r)。
- `Snapshot`：O(n log n)（复制并排序全部元素）。
