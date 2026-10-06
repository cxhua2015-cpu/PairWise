# balanceledger247

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：核心状态是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；逻辑时钟 `generation`/`nextRevision` 与账户表一同由单个 `sync.RWMutex` 保护。`Top`/`Snapshot` 的排序视图在读取时即时物化，不维护冗余有序索引，避免写路径的额外开销。
- **候选事务**：`Apply` 先通过 `ValidateBatch` 做无副作用的完整结构预检（kind、名称、额外字段、Set 绝对值上限），再在写锁内把批次变更暂存到候选暂存区（按首次出现顺序记录 touched 账户）。int64 溢出在算术前检测，绝对值上限在每次 Add/Set 后立即执行，账户容量仅在批次末检查；任一失败直接丢弃暂存区，整体回滚，全部成功才一次性提交并使 `generation` 恰好加一。
- **所有权**：所有公开方法返回的切片均为新分配的副本，与内部状态隔离；`Clone` 在读锁下深拷贝账户表与逻辑时钟，克隆体与原账本互不别名，可独立演进。
- **复杂度**：`Apply` 为 O(k)，k 为批次内操作数（外加 O(k) 暂存空间）；`Top`/`Snapshot` 为 O(n log n)，n 为账户数；`Stats`/`ValidateBatch` 分别为 O(1) 与 O(k)。读路径使用读锁可并发，写路径完全串行化，保证线性一致。
