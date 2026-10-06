# balanceledger232

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 多文件架构

- `creditpool.go` — 核心事务引擎：`Ledger` 状态、`Apply`/`Top`/`Snapshot`。
- `validation.go` — 无副作用批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind、名称字符集与字节上限、Add 非零 delta、绝对值上限）。
- `stats.go` — 线性一致统计 `Stats`（读锁下快照 generation/nextRevision/账户数）。
- `clone.go` — 深拷贝 `Clone`，保留逻辑时钟（generation、nextRevision），所有权完全独立。

## 设计说明

- **索引**：账户主索引为 `map[string]Account`（O(1) 点查/插入/删除）。`Top` 与 `Snapshot` 不做有序索引，按需全量拷贝后排序，以换取事务路径的常数级写入开销。
- **候选事务**：`Apply` 先调用 `ValidateBatch` 做纯结构预检（不读状态），再在写锁内把当前 map 复制为候选状态，按输入顺序在其上执行 Add/Set/Delete；Add 在算术前检测 int64 溢出与绝对值上限，Add/Set 分配连续 revision，最终账户容量仅在批次末检查。任一失败直接丢弃候选状态，实现整体回滚；全部成功才一次性替换内部 map 并将 generation 加一（空批次不增加）。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的副本；`Clone` 重建整个 map，与原账本互不影响。内部状态绝不外泄指针。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch` 持读锁或不读状态，全部公开方法可并发调用。
- **复杂度**：`Apply` 为 O(|ops| + n)，其中 n 为当前账户数（候选复制）；`Top` 为 O(n log n)；`Snapshot` 为 O(n log n)；`Stats` 为 O(1)；`Clone` 为 O(n)；`ValidateBatch` 为 O(|ops|) 且不访问共享状态。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
