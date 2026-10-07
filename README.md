# balanceledger427

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision；int64 溢出在算术前检测并执行绝对值
上限，账户容量仅在批次末检查，失败整体回滚。

## 架构与文件划分

- `creditpool.go`：核心事务引擎。`Ledger` 持有 `map[string]Account` 索引、
  `generation` 与 `nextRevision` 逻辑时钟，由一把 `sync.RWMutex` 保护；
  `Apply`/`Top`/`Snapshot` 与内部共享的 `apply` 执行器均在此。
- `validation.go`：`ValidateBatch`，纯结构预检（kind、名称字符集与字节上限、
  Add 非零 delta），不读取也不修改任何状态，与 `Apply` 共享同一 `validateOp`。
- `stats.go`：`Stats`，在读锁下返回线性一致的 generation/nextRevision/账户数。
- `clone.go`：`Clone`，深拷贝全部账户并保留逻辑时钟，所有权完全独立。
- `preview.go`：`Preview`，在一次读锁（线性化点）内复制状态并复用 `apply`
  完整事务语义，返回候选 `Result`/`Snapshot`/`Stats`；原对象、逻辑时钟与
  所有权均不变，错误及优先级与同状态 `Apply` 完全一致，失败时全部返回零值。

## 索引与候选事务

- 主索引为 `map[string]Account`（按名 O(1) 定位）；`Snapshot` 按名称升序、
  `Top` 按数值降序/名称升序在返回前现排序，不维护冗余有序结构。
- `Apply` 与 `Preview` 都先在账户表的私有副本上执行候选事务，成功才提交
  （`Apply` 交换 map 并推进时钟；`Preview` 直接丢弃副本），因此回滚零成本，
  且失败批次绝不泄漏部分状态。

## 所有权与隔离

- 所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
  均为新建副本，调用方修改不会影响账本内部状态。
- `Clone`/`Preview` 产生的候选对象与原对象不共享任何可写内存。

## 复杂度

- `Apply`/`Preview`：O(k + n)，k 为批内 op 数，n 为账户数（复制 map）。
- `ValidateBatch`：O(k)，无副作用。
- `Top`：O(n log n)；`Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
