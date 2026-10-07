# balanceledger397

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot`
  在读取时物化切片并排序，不维护额外有序索引，因此写入路径保持 O(1) 摊销。
- **候选事务**：`Apply` 先在锁外做完整结构校验（kind、名称字符集与字节上限），
  再在锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。
  任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，原状态不变，
  实现整体回滚；成功时一次性换入候选并推进 `generation` 与 `nextRevision`。
- **所有权**：`Ledger` 内部状态仅由互斥锁保护下的方法访问。`Result.Changed`、
  `Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不影响账本；`Op`/`Batch`
  按值传入，账本不保留调用方切片。
- **复杂度**：设批次长度为 B、账户数为 N。`Apply` 结构校验 O(B)，候选拷贝 O(N)，
  执行 O(B)，容量检查 O(1)，总计 O(N+B)；`Top` 为 O(N log N)；`Snapshot` 为
  O(N log N)（按名称排序）。revision 仅在 Add/Set 上连续分配，Delete 不消耗。
