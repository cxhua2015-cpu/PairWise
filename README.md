# metacatalog266

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位；`Get`/`Snapshot`/`Stats` 走 `sync.RWMutex` 读锁，`Apply` 走写锁，全部公开方法并发安全。
- **候选事务**：`Apply` 先调用 `ValidateBatch` 做无副作用的完整结构预检（不读取任何状态），再在写锁内把记录复制到候选 map，按输入顺序执行 Put/Delete；Put 从候选逻辑时钟分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末对候选状态检查；任何失败直接丢弃候选，状态、generation 与 revision 全部回滚。非空成功批次 generation 只增加一次。
- **所有权**：Put 时拷贝调用方 Value；`Get`/`Snapshot`/`Result.Changed` 均返回深拷贝；`Clone` 复制全部记录与逻辑时钟（generation、nextRevision），与原 Store 完全隔离。`Snapshot` 按名称排序。
- **复杂度**：`Apply` 为 O(R + B)，R 为当前记录数、B 为批大小；`Get` O(1)；`Snapshot`/`Clone` O(R log R)（排序）与 O(R)；`Stats` O(R)；`ValidateBatch` O(B) 且不触碰共享状态。
