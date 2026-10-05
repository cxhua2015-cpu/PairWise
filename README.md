# taskqueue170

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的 `ErrExists` 判重与 Cancel 删除。
- 不维护有序堆：`Pop`/`Snapshot` 时现取就绪集合并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，排序开销可控，实现更简单且不易出错。

### 候选事务（Apply）
Apply 分四个阶段，全程持有同一把互斥锁，因此批次是原子的：
1. **结构校验**：先完整校验 `Now >= 0`、所有 Op 的 kind 合法、ID 非空且只含 `[a-z0-9-_]`、长度不超 `MaxIDBytes`、`ReadyAt >= 0`，此阶段不读取任何队列状态。
2. **时间检查**：`Batch.Now` 必须不小于当前时间，否则 `ErrTime`。
3. **顺序执行**：按序执行 Enqueue（分配单调递增 revision）/Cancel；首次触碰的 ID 会先备份到 `touched` 表，任一 Op 失败（`ErrExists`/`ErrNotFound`）即用备份回滚条目，revision 计数器与时间为局部变量，天然不落盘。
4. **最终容量检查**：只在末尾检查 `len(items) > MaxItems`，超出则整体回滚并返回 `ErrCapacity`；因此批次内“先 Cancel 再 Enqueue”可以合法通过。

成功且非空的批次 `generation` 只加一次；空批次不改变任何状态。失败批次时间、状态、revision 全部回滚。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可并发调用。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，`Item` 为纯值类型，调用方修改返回值不影响队列内部状态。

### 复杂度
- `Apply`：O(k)，k 为批内 Op 数（map 操作均摊 O(1)）。
- `Pop`：O(n + r log r)，n 为当前任务数，r 为就绪任务数（扫描 + 排序），删除为 O(r)。
- `Snapshot`：O(n log n)（复制并按规范顺序排序）。
- 空间：O(n)。
