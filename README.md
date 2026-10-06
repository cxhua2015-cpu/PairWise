# topologygraph273

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 索引设计

`Graph` 内部维护四类索引（见 `topologygraph273/trustgraph.go`）：

- `nodes`：节点名哈希集合，O(1) 存在性判断。
- `edges`：以 `Edge{From, To}` 为键的哈希集合，O(1) 判重。
- `out`：前向邻接索引 `from -> set(to)`，供环检测与 `Reachable` 的 DFS 使用。
- `in`：反向邻接索引 `to -> set(from)`，使 `DeleteNode` 级联删除关联边时无需全表扫描。

## 候选事务

`Apply` 采用“候选事务”模型：先调用与 `ValidateBatch` 共享的纯结构预检（不读状态、无副作用），再在写锁内顺序应用每个操作，同时记录可逆的 undo 日志。任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败（`ErrCapacity`）时，按逆序回放 undo 日志整体回滚，对外无任何可见副作用。非空成功批次 generation 恰好加一；空批次与失败批次不改变 generation。

## 所有权

所有公开方法的返回切片（`Snapshot`、`Stats`、`Clone`）均为全新分配的副本，与内部状态完全隔离：调用方修改返回值不会污染图，后续事务也不会影响已返回的快照。`Clone` 深拷贝全部索引并保留逻辑时钟（generation），克隆体与源图不共享任何内存所有权。

## 复杂度

- `AddNode`/`DeleteEdge`：O(1)。
- `DeleteNode`：O(d)，d 为该节点的关联边数。
- `AddEdge`：O(V+E)，环检测为一次前向可达性 DFS。
- `Apply`：O(Σ 各操作复杂度)，回滚代价与已应用操作数同阶。
- `Reachable`：O(V+E)，在读锁保护的一致快照上执行。
- `Snapshot`/`Clone`：O(V+E)，另加 O(V log V + E log E) 的稳定排序（`Snapshot`）。
- `Stats`：O(1)，读锁保证计数与 generation 线性一致。
