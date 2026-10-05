# taskqueue175

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时按需对候选切片排序。队列规模受 `MaxItems` 上限约束，排序开销可控，实现更简单且不易出错。

### 候选事务（Apply）
`Apply` 分两阶段：
1. **结构校验**：先完整校验整个批次（`Now >= 0`、Kind 合法、ID 字符集与字节上限、`ReadyAt >= 0`），期间不读取任何队列状态。
2. **候选执行**：在队列条目的拷贝（候选 map）上按序执行 Enqueue/Cancel，Enqueue 从候选 `nextRevision` 起分配 revision；容量检查只在所有操作执行完后对最终大小进行一次。

任何一步失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、条目、revision、generation 全部保持不变，实现原子回滚。成功时才一次性提交：推进 `now`、替换条目 map、提交 `nextRevision`，非空批次 `generation` 恰好加一，空批次不变。

### 所有权与并发
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可任意并发调用；`Pop` 的选择与删除在同一临界区内原子完成。
- 返回的切片（`Pop` 结果、`Snapshot.Items`）均为新分配的拷贝，调用方修改不会影响内部状态，反之亦然。
- 时间为显式非负单调值：负值返回 `ErrInvalidInput`，小于当前值返回 `ErrTime`；`Apply` 与 `Pop` 成功时都会推进队列时间。

### 复杂度
设 `n` 为当前条目数，`b` 为批次操作数，`k` 为 Pop 的 limit：
- `Apply`：O(n + b)（拷贝候选 map 并顺序执行操作）。
- `Pop`：O(n log n)（筛选就绪项并排序），删除 O(k)。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
