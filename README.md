# topologygraph228

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
图内部维护三类索引：节点集合 `map[string]struct{}`、边集合 `map[Edge]struct{}`，以及出边邻接表 `out map[string]map[string]struct{}`。邻接表用于环检测与 `Reachable` 的 DFS，边集合用于 O(1) 判重，删除节点时通过边集合扫描清理关联边。

### 候选事务
`Apply` 先用 `ValidateBatch` 做无副作用的完整结构校验（未知 kind、额外字段、非法名称、自环均返回 `ErrInvalidInput`），再在候选副本（节点/边/邻接表的深拷贝）上顺序重放所有操作。任何语义错误（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败都直接丢弃候选，原图零变更；成功则一次性换入候选，非空批次 generation 恰好加一。

### 所有权
所有公开方法由一把 `sync.RWMutex` 保护：写事务持写锁，`Reachable`/`Snapshot`/`Stats`/`Clone` 持读锁，保证线性一致快照。`Snapshot` 返回的切片、`Clone` 返回的图均为全新分配的内存，与内部状态完全隔离，调用方修改不会影响图。`Clone` 同时复制逻辑时钟（generation）。

### 复杂度
设批次含 k 个操作、图含 N 个节点、E 条边。结构校验 O(k·L)（L 为名称长度）；候选复制 O(N+E)；AddNode/DeleteEdge O(1)，DeleteNode O(E)，AddEdge 的环检测为 O(N+E) DFS；容量检查 O(1)。`Reachable` O(N+E)，`Snapshot` O(N log N + E log E)（稳定排序），`Stats` O(1)，`Clone` O(N+E)。
