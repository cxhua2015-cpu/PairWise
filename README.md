# readyqueue410

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检；`Apply` 与 `ValidateBatch` 共享同一个 `validateBatch`，保证结构语义完全一致。
- `stats.go` — 线性一致的 `Stats`：在同一把互斥锁内读取逻辑时钟与条目数。
- `clone.go` — 深拷贝 `Clone`：复制全部逻辑时钟（now/generation/nextRevision）与条目，所有权完全独立。

## 索引

队列以 `map[string]Item` 作为按 ID 的主索引，Enqueue/Cancel/去重均为 O(1) 均摊。弹出时按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对就绪候选排序。

## 候选事务

`Apply` 先在当前条目的私有副本（candidate map）上顺序执行 Enqueue/Cancel 并分配 revision，仅在全部成功且最终容量检查通过后才原子提交（交换 map、推进时间与 revision、generation 恰好加一）。任何失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，时间、状态与 revision 自然回滚。空批次为无操作，generation 不变。

## 所有权

所有公开方法返回的切片（`Pop`、`Snapshot`）均为新分配的副本，与内部状态隔离；`Clone` 逐条复制条目，克隆体与原队列互不影响。全部公开方法由单把互斥锁保护，可并发调用。

## 复杂度

- `Enqueue`/`Cancel`（单次操作）：均摊 O(1)。
- `Apply`（k 个操作、n 个条目）：O(n + k)，候选副本复制 O(n)。
- `Pop`（r 个就绪条目）：O(n + r log r)。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
