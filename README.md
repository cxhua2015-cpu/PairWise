# balanceledger422

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，`Add`/`Set` 分配连续 revision；int64 溢出在算术前检测，
绝对值上限逐操作执行，账户容量仅在批次末检查，失败整体回滚。

## 架构

- `creditpool.go` — 核心事务引擎：`Ledger`（`sync.RWMutex` + `map[string]Account`
  索引 + generation/nextRevision 逻辑时钟）、`Apply`、`Top`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一
  `validateBatch` 结构语义（kind、名称字符集与字节上限、非零 delta），只读锁下
  不读取账户状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁内返回 `{Generation, NextRevision,
  Accounts}`。
- `clone.go` — 深拷贝：`Clone` 在读锁内复制全部账户与逻辑时钟，新对象拥有独立
  map 与互斥锁，与原对象完全无共享所有权。
- `preview.go` — 候选事务：`Preview` 在一次读锁（线性化点）内把当前状态复制到
  候选 map，复用与 `Apply` 完全相同的 `applyBatch` 引擎，返回候选 `Result`、
  `Snapshot`、`Stats`；原对象状态、generation、revision 与逻辑时间均不变，
  错误及优先级与同状态 `Apply` 一致，失败时全部返回零值。

## 索引与所有权

- 主索引为 `map[string]Account`（按名称 O(1) 定位）；`Top`/`Snapshot` 在读取时
  排序，不维护额外有序结构。
- 所有公开方法在返回前拷贝切片，返回的 `[]Account`/`Snapshot` 与内部状态隔离；
  `Clone`/`Preview` 的候选状态同样不共享任何可写内存。

## 复杂度

- `Apply`/`Preview`：O(n + a)，n 为批内操作数，a 为当前账户数（候选 map 复制）。
- `Top`：O(a log a)；`Snapshot`：O(a log a)（按名称排序）。
- `Stats`：O(1)；`ValidateBatch`：O(n·L)，L 为名称长度；`Clone`：O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
