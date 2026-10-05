# taskqueue190

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、插入与删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久堆结构，而是在 `Pop`/`Snapshot` 时对当前条目一次性排序；队列规模受 `MaxItems` 上限约束，排序开销可控。

**候选事务（Apply）**
- `Apply` 先校验时间（非负、单调），再对整个批次做完整结构校验（kind、ID 字符集与长度、ReadyAt 非负），不读取任何状态。
- 校验通过后克隆当前 `items` 与 `nextRevision` 作为候选状态，按序执行 Enqueue（分配递增 revision）/Cancel；任一步失败（`ErrExists`/`ErrNotFound`）直接丢弃候选状态。
- 容量检查只在末尾进行：批次中间的 Cancel 可为后续 Enqueue 腾出空间。最终 `len(items) > MaxItems` 时返回 `ErrCapacity` 并整体回滚。
- 仅在全部成功时提交：替换 `items`、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一；空批次只推进时间，generation 不变。失败路径不触碰时间、状态与 revision。

**所有权**
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 在选择与删除上是原子的；`Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- 时间为显式非负单调值：`now` 小于当前时间返回 `ErrTime`，负数返回 `ErrInvalidInput`；成功的 `Apply`/`Pop` 都会推进时间。

**复杂度**（n = 当前条目数，k = 批次内操作数，m = Pop 上限）
- `Apply`：结构校验 O(k)，克隆与执行 O(n + k)，末尾容量检查 O(1)。
- `Pop`：排序 O(n log n)，选取并删除至多 m 个就绪任务 O(m)。
- `Snapshot`：O(n log n)（排序副本）。
- 空间：O(n)。
