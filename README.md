# balanceledger237

并发安全的内存型余额账本，仅依赖 Go 标准库（Go 1.22+）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚；详见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`。
- `validation.go` — 无副作用批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义
  （kind 合法、字段纪律、名称字母表与字节上限、幅值上限）。
- `stats.go` — 线性一致的状态摘要 `Stats`（读锁下生成）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）的深拷贝 `Clone`。

## 设计说明

- **索引**：账户主存储为 `map[string]Account`，按名 O(1) 定位。`Top` 与 `Snapshot`
  不维护有序索引，而在读锁内对快照切片排序——`Top` 按值降序、名称升序，
  `Snapshot` 按名称升序，避免写路径为有序结构付出额外代价。
- **候选事务**：`Apply` 先做一次完整结构预检（不读状态），再在写锁内把账户表
  复制为候选副本，按输入顺序在其上执行全部操作并分配连续 revision；任何错误
  （溢出、绝对值上限、ErrNotFound、批次末容量检查）直接丢弃候选副本，已提交
  状态与逻辑时钟完全不变，实现整体回滚。空批次成功且不推进 generation。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot`）均为
  新建副本，与内部状态隔离；`Clone` 复制整张账户表与逻辑时钟，克隆体与原账本
  互不影响。内部状态仅由 `sync.RWMutex` 保护，写操作独占、读操作共享。
- **复杂度**：设批次含 b 个操作、账户总数为 n。`Apply` 为 O(b + n)（候选复制），
  `ValidateBatch` 为 O(b)，`Top` 为 O(n log n)，`Snapshot` 为 O(n log n)，
  `Stats` 为 O(1)，`Clone` 为 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
