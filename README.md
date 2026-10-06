# readyqueue295

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

队列内部使用 `map[string]Item` 按 ID 索引，Enqueue/Cancel 的去重与查找为 O(1)。`Pop` 在弹出时收集 `ReadyAt <= now` 的候选集，按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序后截取前 `limit` 个并原子删除；`Snapshot` 同样以规范顺序返回。

### 候选事务

`Apply` 先用与 `ValidateBatch` 共享的同一套结构预检（无副作用、不读状态），再在互斥锁内把批次顺序应用到一份私有候选 map 上：Enqueue 分配递增 revision，Cancel 删除。只有在全部操作成功且**最终**容量不超过 `MaxItems` 时才一次性提交时间、状态与 revision 时钟；任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）都直接丢弃候选，实现完整回滚。非空成功批次 generation 恰好加一，空批次只推进时间、不改变 generation。

### 所有权

所有公开方法返回的数据（`Pop`/`Snapshot` 的切片、`Clone` 的新队列）都与内部状态完全隔离：`Snapshot`/`Pop` 返回新分配的切片，`Clone` 重建 item map 并按值复制每个 `Item`，同时保留逻辑时钟（now、generation、nextRevision）。克隆体与原队列互不影响。

### 并发与复杂度

单个 `sync.Mutex` 串行化所有公开方法，因此 `Apply`、`Pop`、`Stats`、`Snapshot`、`Clone` 之间都是线性一致的；`ValidateBatch` 只读不可变配置，无需持锁。复杂度：Enqueue/Cancel 均摊 O(1)，`Apply` 为 O(batch + n)（候选拷贝），`Pop`/`Snapshot` 为 O(n log n)（候选排序），`Stats` 为 O(1)，`Clone` 为 O(n)。

### 文件分工

- `prioritybox.go`：核心事务引擎（`New`/`Apply`/`Pop`/`Snapshot`）
- `validation.go`：共享结构预检 `ValidateBatch`
- `stats.go`：线性一致统计 `Stats`
- `clone.go`：保留时钟、所有权隔离的 `Clone`
