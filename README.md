# readyqueue385

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 不维护持久堆；`Pop` 与 `Snapshot` 在临界区内现算规范顺序（Priority 降序、ReadyAt 升序、ID 升序）。队列规模受 `MaxItems` 上限约束，排序成本可控。

**候选事务（Apply）**
- 批次先做完整结构校验（`Now >= 0`、kind 合法、ID 合法、`ReadyAt >= 0`），再检查单调时间，然后才读取状态。
- 非空批次在主索引的克隆（候选状态）上顺序执行 Enqueue/Cancel：Enqueue 从单调计数器分配 revision，Cancel 删除条目；最终容量只在末尾检查。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选状态，时间、条目与 revision 计数器全部天然回滚；成功才一次性提交，generation 恰好加一。空批次不修改任何状态。

**所有权与并发**
- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用；`Apply`/`Pop` 的原子性由互斥锁保证。
- `Pop` 在临界区内筛选、排序并原子删除，返回的是新分配的切片；`Snapshot` 返回排序后的副本。返回数据与内部状态完全隔离，调用方可自由修改。
- 时间为显式非负单调值：`Apply.Now` 与 `Pop` 的 `now` 不得小于队列当前时间，否则返回 `ErrTime` 且不产生副作用。

**复杂度**（n = 当前条目数，b = 批次大小）
- `Apply`：O(n + b)，克隆与顺序执行；容量检查 O(1)。
- `Pop`：O(n log n)，筛选就绪项并排序，删除 O(k)（k 为弹出数）。
- `Snapshot`：O(n log n)，复制并排序。
- 空间：O(n)。
