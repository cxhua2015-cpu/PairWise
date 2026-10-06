# readyqueue300

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`，以及规范排序 `sortItems`（Priority 降序、ReadyAt 升序、ID 升序）。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`。`Apply` 与 `ValidateBatch` 共享同一个 `validateBatch` 函数，保证预检与事务的结构语义完全一致。
- `stats.go` — `Stats` 在读锁下一次性读取所有计数器，提供线性一致的状态摘要。
- `clone.go` — `Clone` 在读锁下深拷贝全部字段（含 generation、nextRevision、now 等逻辑时钟），返回完全独立的队列。

## 索引

主索引是 `map[string]Item`，按 ID 提供 O(1) 的存在性判断（`ErrExists` / `ErrNotFound`）与删除。就绪选择不做增量堆维护：`Pop`/`Snapshot` 时把候选收集成切片后用 `sortItems` 做规范排序。这以 O(n log n) 的弹出成本换取事务回滚的极简实现。

## 候选事务（candidate transaction）

`Apply` 先完整结构校验（不读状态），再在写锁内把当前 map 复制为候选副本，按顺序在副本上执行 Enqueue/Cancel 并单调分配 revision；最终容量只在末尾检查一次。任一失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选——时间、条目与 revision 计数器天然回滚，无需补偿日志。全部成功才一次性提交：替换 map、推进 `now`、generation 恰好加一；空批次不改变任何状态。

## 所有权

- 所有公开方法在 `sync.RWMutex` 保护下执行，可并发调用；写操作（`Apply`/`Pop`）互斥，读操作（`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）可并行。
- 返回给调用方的切片（`Pop`、`Snapshot.Items`）均为新分配的副本，与内部 map 无共享；`Clone` 复制整个 map，克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(n + k)，n 为当前条目数（候选复制），k 为批次内 op 数。
- `Pop`：O(n log n)，n 为就绪候选数；原子删除被选中的条目。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(k)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
