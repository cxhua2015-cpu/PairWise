# readyqueue270

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

队列内部维护两个始终一致的索引（`readyqueue270/prioritybox.go`）：

- `byID map[string]*entry`：按 ID 的哈希索引，Enqueue 去重与 Cancel 定位均为 O(1)。
- `ready readyHeap`：按 `(ReadyAt 升序, ID 升序)` 排列的最小堆，堆顶永远是就绪时间最早的任务；每个 `entry` 记录其堆下标，因此 Cancel 的堆删除为 O(log n)。

### 候选事务

`Apply` 先调用 `ValidateBatch` 做完整结构预检（不读状态、无副作用，`readyqueue270/validation.go`），再在互斥锁内把整批操作顺序应用到当前状态上，同时记录撤销日志。容量上限只在所有操作执行完毕后检查一次；任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）都会逆序回放撤销日志并恢复 revision 计数，时间、状态与 revision 一并回滚，批次原子生效。非空成功批次 generation 恰好加一，空批次不变。

`Pop` 先从就绪堆中取出所有 `ReadyAt <= now` 的候选任务，再按 `Priority 降序、ReadyAt 升序、ID 升序` 排序，原子删除前 `limit` 个，其余候选重新入堆。

### 所有权

所有公开方法（`Apply`/`Pop`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）都可并发调用：可变状态由同一把互斥锁保护，`Stats`（`readyqueue270/stats.go`）与 `Snapshot` 因此是线性一致的。`Snapshot` 返回的切片按 ID 排序且为全新拷贝；`Clone`（`readyqueue270/clone.go`）深拷贝全部条目、索引与逻辑时钟（now、generation、nextRevision），克隆体与原队列不共享任何内存，互不影响。

### 复杂度

- `Enqueue`/`Cancel`：O(log n)（堆插入/删除 + 哈希索引）。
- `Apply`（k 个操作）：O(k log n)，失败回滚同为 O(k log n)。
- `Pop`（取出 m 个、候选 c 个）：O(c log n + c log c)。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n log n)。
- `ValidateBatch`：O(批次字节数)，不触碰队列状态。
