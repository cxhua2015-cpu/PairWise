# balanceledger367

并发安全的内存型余额账本，实现见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

**索引**：核心状态为 `map[string]Account`，按名称 O(1) 定位账户。`Top` 与 `Snapshot` 在读取时把 map 物化为切片并排序（分别为值降序/名称升序、名称升序），不维护额外的有序索引——账户数受 `MaxAccounts` 上限约束，排序成本可控，且避免写路径上的索引维护开销。

**候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节长度），再在互斥锁内把账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。Add 在做加法前先检测 int64 溢出，随后对结果执行绝对值上限检查；Add/Set 从单调递增的 `nextRevision` 分配连续 revision。任一步失败直接返回，候选被丢弃，状态、revision、generation 全部不变，实现整体回滚。账户容量仅在批次末尾对候选大小检查一次，因此批次内"先删后建"可以越过容量。非空批次成功提交时 generation 恰好加一；空批次不改变任何状态。

**所有权**：`Ledger` 内部账户表绝不外泄。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为每次调用新建的副本，调用方修改返回值不影响内部状态。所有公开方法通过 `sync.RWMutex` 保护：写路径（`Apply`）持写锁，读路径（`Top` 的拷贝阶段、`Snapshot`）持读锁，可并发执行。

**复杂度**：设批次含 k 个 op、当前 n 个账户。`Apply` 为 O(n + k)（候选拷贝加顺序执行）；`Top(m)` 为 O(n log n)；`Snapshot` 为 O(n log n)；`New` 为 O(1)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
