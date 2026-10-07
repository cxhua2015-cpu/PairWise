# balanceledger417

并发安全的内存型“余额账本 417”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为四个联动文件，共享同一套结构语义与逻辑时钟：

- `creditpool.go` — 核心事务引擎：`Ledger` 类型、`New`/`Apply`/`Top`/`Snapshot`。
- `validation.go` — 无副作用的批次预检 `ValidateBatch`；`Apply` 在读取任何状态之前
  先调用同一函数，保证“预检通过 ⇔ 事务可进入执行阶段”。
- `stats.go` — 线性一致的状态摘要 `Stats`，在读锁内一次性采样三个时钟/计数。
- `clone.go` — 保留逻辑时钟（generation、nextRevision）的深拷贝 `Clone`。

## 索引

账本内部只维护一个权威索引：`map[string]Account`（名称 → 账户），由
`sync.RWMutex` 保护。`Top` 与 `Snapshot` 不维护额外的有序结构，而是按需将索引
物化为新切片并排序（分别按 值降序/名称升序 与 名称升序），以换取写入路径 O(1)。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构预检**：调用 `ValidateBatch`，不读取任何状态；未知 kind、非法名称、
   多余字段、零操作数在此拒绝（`ErrInvalidInput`/`ErrValue`）。
2. **候选执行**：在写锁内把索引浅拷贝为候选 map，按输入顺序执行 Add/Set/Delete；
   Add/Set 各消耗一个连续 revision。算术前先检测 int64 溢出，再执行绝对值上限；
   账户容量上限仅在批次末对候选索引检查。任一失败直接丢弃候选 map —— 回滚是
   免费的，原状态与两个逻辑时钟完全不变。全部成功才一次性提交：交换索引、
   generation 恰好 +1、nextRevision 前进到候选终点。

## 所有权

所有公开方法返回的数据（`Result.Changed`、`Top`、`Snapshot`、`Clone`）都是新分配
的切片/map 副本，调用方拥有独占所有权；修改返回值不会影响账本，账本后续的
变更也不会影响已返回的快照。`Clone` 复制全部状态与逻辑时钟，克隆体与原账本
互不影响。

## 复杂度

设 n = 账户数，b = 批次大小：

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply` | O(b + n)（候选拷贝） | O(n) |
| `ValidateBatch` | O(b) | O(1) |
| `Top(k)` | O(n log n) | O(n) |
| `Snapshot` | O(n log n) | O(n) |
| `Stats` | O(1) | O(1) |
| `Clone` | O(n) | O(n) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
