# resourceledger162

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主体为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot` 不维护额外有序索引，而是在读取时把 map 物化为切片并排序——写入路径保持 O(1)，排序成本只在读取时支付。

**候选事务**：`Apply` 分两阶段。第一阶段在不持锁的情况下对全部操作做结构校验（kind 合法、名称字符集与长度）；第二阶段持锁把账户表浅拷贝为候选 map（`Account` 为值类型，拷贝即隔离），按输入顺序在其上执行 Add/Set/Delete，Add/Set 分配连续 revision。任一步失败（溢出/绝对值上限 `ErrValue`、删除不存在 `ErrNotFound`、批次末容量 `ErrCapacity`）直接丢弃候选 map，实现零成本整体回滚；成功则一次性换入候选 map，非空批次 generation 恰好加一。

**所有权**：所有公开方法由单一 `sync.Mutex` 保护，可并发调用。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Changed` 按账户去重（同一账户多次修改只记录最终值），保留首次触达顺序。

**复杂度**：设批次含 k 个操作、账本含 n 个账户。`Apply` 结构校验 O(k)，执行 O(k + n)（候选拷贝），容量检查 O(1)；`Top(m)` 与 `Snapshot` 均为 O(n log n) 时间、O(n) 额外空间。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
