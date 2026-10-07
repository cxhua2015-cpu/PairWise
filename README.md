# balanceledger377

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；
revision 是单调递增的 `uint64` 计数器（`nextRevision`），不单独建索引。
`Top` 与 `Snapshot` 在读取时按需排序，不维护有序结构，以换取写入路径的 O(1) 更新。

**候选事务**：`Apply` 先做整批结构校验（kind 合法、名称字符集与字节上限），
再在账户表的克隆（候选副本）上按输入顺序执行 Add/Set/Delete。溢出在算术前检测，
绝对值上限逐操作执行，账户容量仅在批次末对候选副本检查。任一失败直接丢弃候选副本，
账本状态、generation、revision 计数器完全不变，实现整体回滚；全部成功才一次性提交。

**所有权**：所有公开方法由单个 `sync.Mutex` 保护，可并发调用。`Result.Changed`、
`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；
`Changed` 按首次触碰顺序去重，记录账户最终状态（批次内先建后删的账户不出现，
删除批次前已存在的账户会以其删除前状态出现）。

**复杂度**：设批次长度为 B、账户数为 N、Top 参数为 K。
`Apply` 结构校验 O(B)，执行 O(B)，候选克隆 O(N)，容量检查 O(1)；
`Top` 为 O(N log N)（排序后取前 K）；`Snapshot` 为 O(N log N)（按名称排序）；
单次 Add/Set/Delete 的状态更新为 O(1) 均摊。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
