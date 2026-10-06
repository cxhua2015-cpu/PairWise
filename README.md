# readyqueue250

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。核心语义见 `SPEC.md`。

## 多文件架构

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`，以及排序与错误值。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 复用同一 `validateBatch`，保证预检与事务结构语义完全一致。
- `stats.go` — 线性一致的 `Stats`：在同一把互斥锁内读取 generation、nextRevision、now 与条目数。
- `clone.go` — 深拷贝 `Clone`：复制全部逻辑时钟（generation、nextRevision、now）与条目，所有权完全独立。

## 索引

队列以 `map[string]Item` 作为主索引，Enqueue/Cancel 按 ID O(1) 定位。弹出序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，而是在 `Pop`/`Snapshot` 时对候选集即时排序——容量有界（`MaxItems`），实现简单且避免索引不一致。

## 候选事务

`Apply` 先在候选副本（复制的 map 与本地 revision 计数）上顺序执行 Enqueue/Cancel，最终容量只在末尾检查；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、状态与 revision 均不发生任何变化。全部成功才一次性提交：替换 map、推进 `now`、generation 恰好加一（空批次不变）。

## 所有权

所有公开方法在单个 `sync.Mutex` 下串行化，可并发调用。`Snapshot`/`Pop` 返回的切片均为新分配的副本，`Clone` 复制整个 map，调用方与队列内部状态完全隔离，克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制），m 为批次操作数。
- `Pop`：O(n log n) 排序就绪候选，删除 O(k)，k 为弹出数。
- `Snapshot`：O(n log n)；`Stats`/`ValidateBatch`：O(1)/O(m)；`Clone`：O(n)。
- 空间：O(n)。
