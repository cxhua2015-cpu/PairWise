# balanceledger422

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构与多文件联动

- `creditpool.go` — 核心事务引擎：`Ledger`（`sync.RWMutex` + `map[string]Account` 索引）、`Apply`、`Top`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 只做结构校验（kind、名称字符集/字节上限、Add 非零 Delta、额外字段必须为零），不读状态、不加锁；`Apply` 在执行前复用同一套结构语义。
- `stats.go` — `Stats` 在读锁下返回线性一致的 `{Generation, NextRevision, Accounts}` 摘要。
- `clone.go` — `Clone` 在读锁下深拷贝账户映射并保留逻辑时钟（generation、nextRevision），副本与原对象完全隔离。
- `preview.go` — `Preview` 在 `Clone` 得到的候选事务上执行完整 `Apply` 语义，返回候选 `Result`/`Snapshot`/`Stats`；原对象的状态、generation、revision 与逻辑时间不变，失败时返回与 `Apply` 相同的错误且全部返回值为零值。

## 索引与事务

- 账户主索引为 `map[string]Account`，按名 O(1) 定位；`Top`/`Snapshot` 在读取时物化并排序，不维护额外有序结构。
- 原子批次：先整体结构校验，再在写锁内于工作副本（map 拷贝）上按输入顺序执行 Add/Set/Delete；Add/Set 分配连续 revision；int64 溢出与绝对值上限在算术前检测；账户容量仅在批次末检查；任一失败整体回滚，时钟不前进。
- 候选事务：`Preview` = `Clone` + 候选 `Apply`，错误及优先级与同一状态上的真实提交完全一致。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的副本；`Clone` 复制整个映射。调用方修改返回值不会影响账本内部状态，反之亦然。

## 复杂度

设 n 为账户数、k 为批次数：

- `Apply` / `Preview`：O(n + k) 时间与 O(n + k) 额外空间（工作副本）。
- `ValidateBatch`：O(k·L)，L 为名称长度；不读状态。
- `Top`：O(n log n)；`Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
