# balanceledger382

并发安全的内存型余额账本，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户主索引为 `map[string]Account`，按名称 O(1) 定位。`Top` 与
  `Snapshot` 不维护辅助有序结构，而是在读路径上对当前账户快照按需排序
  （`sort.Slice`），写路径因此保持 O(1) 均摊。
- **候选事务**：`Apply` 先对全部操作做纯结构校验（kind、名称字符集与字节
  上限），不触碰状态；随后在写锁内把主索引复制为候选 map，按输入顺序在
  候选上执行 Add/Set/Delete。任一步失败（`ErrValue`/`ErrNotFound`/
  `ErrCapacity`）直接丢弃候选，主状态零改动，实现整体回滚；成功时一次性
  换入候选并提交 generation 与 revision。
- **所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片及其中的
  `Account` 值均为新分配的副本，调用方修改不会影响账本内部状态。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Top` 与
  `Snapshot` 取读锁，所有公开方法可安全并发调用。
- **复杂度**：设批次长度为 B、账户数为 N。`Apply` 结构校验 O(B)，候选复制
  O(N)，执行 O(B)，容量检查 O(1)；`Top` 与 `Snapshot` 为 O(N log N) 排序，
  空间 O(N)。revision 仅在 Add/Set 上连续分配，Delete 不消耗 revision；
  非空成功批次 generation 恰好加一，空批次不改变任何计数器。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
