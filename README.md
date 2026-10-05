# rewardledger

并发安全的内存型奖励积分账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户。不维护有序索引；`Top` 与 `Snapshot` 在调用时对当前账户快照做一次性排序（`Top` 按数值降序、名称升序，`Snapshot` 按名称升序），避免写路径上的额外维护开销。

**候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在候选副本（candidate map）上按输入顺序执行 Add/Set/Delete。算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限；最终账户容量仅在批次末检查。任何一步失败直接丢弃候选集，主状态、revision、generation 均不变，实现整体回滚；全部成功才一次性提交。

**所有权与并发**：所有公开方法由单把 `sync.Mutex` 保护，可安全并发调用。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，与内部状态完全隔离，调用方修改不影响账本。revision 从 1 起连续分配，非空成功批次 generation 恰好加一，空批次不改变任何计数器。

**复杂度**：设批次长度为 B、账户数为 N、返回条数为 K。`Apply` 为 O(B) 时间、O(B) 额外空间；`Top` 为 O(N log N) 时间、O(N) 空间（截取前 K 条）；`Snapshot` 为 O(N log N) 时间、O(N) 空间；`New` 为 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
