# balanceledger372

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户主索引为 `map[string]Account`（按名称 O(1) 查找）。`Top` 与 `Snapshot` 不维护有序索引，而是每次调用时对当前账户快照就地排序——账户数受 `MaxAccounts` 上限约束，排序开销可控，且避免了写路径上维护堆/树序的复杂度。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在写锁内把主索引浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，主状态、revision、generation 完全不变，实现整体回滚；全部成功才用候选替换主索引。int64 溢出与绝对值上限在每次算术前检测，账户容量上限仅在批次末检查。
- **所有权**：`Account` 为纯值类型，候选拷贝即深拷贝。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建切片，调用方修改不会泄漏进内部状态；内部也绝不持有调用方传入的切片。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Top`/`Snapshot` 取读锁。revision 从 1 起连续分配（仅 Add/Set 消耗），非空成功批次 generation 恰好 +1，空批次不改变任何状态。
- **复杂度**：设批次长度 B、账户数 N。`Apply` 为 O(N + B)（候选拷贝 + 顺序执行）；`Top(k)` 为 O(N log N)；`Snapshot` 为 O(N log N)（按名称排序）。空间 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
