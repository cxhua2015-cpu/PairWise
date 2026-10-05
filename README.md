# controlgraph093

并发安全的内存型控制面依赖图。读取 `SPEC.md` 了解语义；仅依赖标准库，需 Go 1.22+。

## 设计说明

- **索引**：图状态由四份索引组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）、`out`（出邻接表）、`in`（入邻接表）。邻接表用于环检测与可达性 DFS，以及 `DeleteNode` 时 O(度数) 级联删除关联边。
- **候选事务**：`Apply` 先做纯结构校验（不读状态），再在写锁内把当前状态克隆为候选副本，按序在副本上执行全部操作；任一步失败或批次末容量（节点/边上限）超限即丢弃副本，整体回滚，已持有的状态不受影响。仅在全部成功时用副本原子替换正式状态，非空成功批次 `generation` 恰好加一，空批次不变。
- **所有权与并发**：所有公开方法可在任意 goroutine 并发调用。`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，读取的是同一份一致快照。`Snapshot` 对节点按字典序、边按 `(From, To)` 稳定排序，且返回的切片为新建副本，调用方修改返回值不影响内部状态。
- **复杂度**：设批次含 B 个操作、图有 V 个节点、E 条边。结构校验 O(B·L)（L 为名称长度）；候选克隆 O(V+E)；`AddNode`/`DeleteNode`/`AddEdge`/`DeleteEdge` 均摊 O(1)（`DeleteNode` 另加 O(度数)）；每次 `AddEdge` 的环检测为一次 DFS，O(V+E)；容量检查 O(1)。`Reachable` 为一次 DFS，O(V+E)；`Snapshot` 为 O(V log V + E log E)。
