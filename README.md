# balanceledger267

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：`Ledger` 以 `map[string]Account` 为主索引，按名称 O(1) 定位账户；`Top` 与 `Snapshot` 在读取时物化并排序副本，不维护额外的有序结构，因此写入路径保持 O(1) 摊销。
- **候选事务**：`Apply` 先通过 `ValidateBatch` 做无副作用的完整结构预检（kind、名称字符集与字节上限、Add 非零 delta），再在互斥锁内把账户表复制为候选副本，按输入顺序执行 Add/Set/Delete。int64 溢出在算术之前检测，绝对值上限逐操作执行，账户容量上限仅在批次末检查；任一失败直接丢弃候选副本，实现整体回滚。成功时一次性提交并令 generation 递增一次，Add/Set 分配连续 revision。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot`）均为新分配的副本；`Clone` 在读锁下深拷贝账户表与逻辑时钟（generation、nextRevision），克隆体与原账本完全独立，互不影响。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Top`/`Snapshot`/`Stats`/`Clone` 取读锁，`ValidateBatch` 不读状态。`Stats` 与 `Snapshot` 因此是线性一致的一致视图。
- **复杂度**：单操作批次 O(1) 摊销；含 k 个操作、n 个账户的批次为 O(n + k)（候选复制 + 顺序执行）；`Top`/`Snapshot` 为 O(n log n)；`Stats` 为 O(1)；`Clone` 为 O(n)。
