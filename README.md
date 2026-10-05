# taskqueue100

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `byID map[string]*entry`：所有权索引，按 ID O(1) 定位任务，用于 Enqueue 去重与 Cancel。
- `ready readyHeap`：按 `(ReadyAt 升序, ID 升序)` 的最小堆（`container/heap`），与 `byID` 共享同一份 `entry`，堆内索引 `idx` 支持 O(log n) 任意删除。

### 候选事务（Pop）
Pop 先从堆中筛出 `ReadyAt <= now` 的候选，再按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序取前 k 个，最后在同一临界区内从两个索引原子删除并推进时间。

### 所有权与回滚
每个任务只有一份 `entry`，两个索引仅持有指针；返回值与 `Snapshot.Items` 均为拷贝，与内部状态隔离。Apply 在互斥锁内顺序执行 Enqueue/Cancel，Enqueue 就地分配递增 revision；失败时按逆序撤销已应用的 op 并恢复 `nextRevision`。容量只在批次末尾对最终状态检查，超限则整体回滚（时间、状态、revision 均不变）。非空成功批次 generation 恰好加一，空批次不改变任何状态。

### 并发
所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用。

### 复杂度（n = 队列任务数，m = 批次 op 数，c = Pop 候选数，k = Pop 上限）
- `Apply`：校验 O(m)，执行 O(m log n)，失败回滚同阶；容量检查 O(1)。
- `Pop`：候选收集 O(n)，排序 O(c log c)，删除 O(k log n)。
- `Snapshot`：O(n log n)（拷贝并按规范顺序排序）。
- `New`：O(1)。
