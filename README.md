# readyqueue250

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：`Queue` 以 `map[string]Item` 作为主索引，ID 查找/去重为 O(1)；`Pop` 时扫描就绪项（`ReadyAt <= now`）并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，单次 `Pop` 为 O(n log n)，n 为就绪项数量。`Snapshot` 同样按规范顺序返回。
- **候选事务**：`Apply` 先通过 `ValidateBatch` 做完整结构预检（不读状态），再在锁内将操作顺序应用到候选 map 副本上；任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）都会丢弃副本，时间、状态与 revision 分配器全部回滚。成功时一次性提交：替换索引、推进逻辑时钟、非空批次 generation 恰好加一。
- **所有权**：所有返回的切片（`Pop`、`Snapshot`）都是新建副本，与内部状态隔离；`Clone` 在深拷贝全部条目的同时保留逻辑时钟（now、generation、nextRevision），克隆体持有独立互斥锁，与原始队列互不影响。
- **并发与复杂度**：单把 `sync.RWMutex` 保护全部状态；`Apply`/`Pop` 取写锁，`Snapshot`/`Stats`/`Clone` 取读锁，因此统计与克隆都是线性一致的。`Enqueue`/`Cancel` 均摊 O(1)，批次 Apply 为 O(k + n)（k 为操作数，候选复制 O(n)），`ValidateBatch` 为 O(k)，`Stats` 为 O(1)。
