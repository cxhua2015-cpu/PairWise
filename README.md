# creditpool

并发安全的内存型信用额度池（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Ledger` 以 `map[string]Account` 作为主索引，按名称 O(1) 定位账户；`Top` 与 `Snapshot` 在读取时按需对账户切片排序，不维护额外的有序结构，从而保持写入路径简单。

**候选事务**：`Apply` 先对全部操作做纯结构校验（kind、名称字符集与字节长度），不触碰状态；随后在账户表的浅拷贝（候选副本）上按输入顺序执行 Add/Set/Delete。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）即丢弃副本，整体回滚；仅当批次末容量检查通过时才用副本原子替换内部表，并将 generation 递增一次。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，调用方修改不会影响内部状态；内部状态只在持有写锁时变更。

**并发**：单把 `sync.RWMutex` 保护全部状态——`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可多读者并行。

**复杂度**（n = 账户数，k = 批次数）：
- `Apply`：结构校验 O(k)，候选拷贝 O(n)，执行 O(k)，合计 O(n + k)。
- `Top`：O(n log n) 排序后取前 m 个。
- `Snapshot`：O(n log n) 按名称排序。
- 空间：O(n)。

**边界**：Add 在算术前检测 int64 溢出；每个 Add/Set 结果立即执行绝对值上限（`ErrValue`）；账户容量仅在批次末检查（`ErrCapacity`）；revision 由 Add/Set 连续分配，Delete 不消耗 revision。
