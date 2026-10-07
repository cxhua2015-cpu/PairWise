# topologygraph438

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：`Graph` 持有 `nodes map[string]struct{}` 与 `edges map[Edge]struct{}` 两个哈希索引，节点/边存在性判断为 O(1)；`Snapshot` 在读取时对节点按键序、边按 `(From, To)` 字典序稳定排序。可达性用迭代式 DFS，避免递归溢出。
- **候选事务**：`Apply` 先在 `validateStructural` 中完成整批结构校验（不读状态），再把节点/边索引复制为候选状态，在候选上按序模拟全部操作；容量上限（`MaxNodes`/`MaxEdges`）只在批次末检查。任一失败直接丢弃候选，原状态零改动，实现整体回滚；成功时原子替换索引并将 `generation` 加一（空批次不变）。
- **所有权**：所有公开方法在 `sync.RWMutex` 下线性化；`Snapshot`/`Stats` 返回新分配的切片与值，与内部状态隔离。`Clone` 在读锁下深拷贝索引与逻辑时钟（`generation`），克隆体与原对象互不影响。`Preview` 在一致快照上克隆后完整复用 `Apply` 事务语义，返回候选 `Result`/`Snapshot`/`Stats`，错误及优先级与同状态 `Apply` 完全一致，且不改原对象的状态、时钟或所有权。
- **复杂度**：设批次含 `k` 个操作、图含 `N` 个节点、`E` 条边。结构校验 O(k·L)（L 为名称长度）；`Apply` 复制索引 O(N+E)，模拟每操作 O(1)（`AddEdge` 的环检测与 `DeleteNode` 的关联边清理为 O(E)），整体 O(N + E + k·E)；`Reachable` O(N+E)；`Snapshot` O(N log N + E log E)；`Stats` O(1)；`Clone`/`Preview` O(N+E) 加一次候选事务。
