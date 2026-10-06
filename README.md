# topologygraph213

并发安全的内存型有向无环控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引结构

`Graph` 持有四份相互一致的内存索引，全部受一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：边集合，键为 `{From, To}`。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 BFS 与环检测。
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(关联边数) 级联删除。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态之前校验整个批次（kind 合法、名称非空且仅含
   `[a-z0-9-_]`、不超过 `MaxNameBytes`、字段不冗余），失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把四份索引深拷贝为 candidate，按序在 candidate 上执行
   全部操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃 candidate。
   节点/边容量只在批次末尾对最终状态检查，超限返回 `ErrCapacity` 并整体回滚。
   全部成功才将 candidate 指针换入 `Graph`，非空成功批次 `generation` 恰好加一，
   空批次不变。因此批次是原子的：调用者永远观察到完整提交或完全未变的状态。

## 所有权与并发

- 所有公开方法可并发调用：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁。
- `Snapshot` 返回的切片与 `Result`/`Snapshot` 值均为新建副本，调用方可自由修改，
  不会泄漏内部状态；`Reachable` 在读锁内对当前一致快照做 BFS。
- `Snapshot` 的 `Nodes` 按字典序、`Edges` 按 `(From, To)` 字典序稳定排序。

## 复杂度

设 N 为节点数、E 为边数、B 为批次数、D 为被删节点的关联边数：

- `Apply`：结构校验 O(B·名称长度)；候选拷贝 O(N+E)；逐操作 O(1) 均摊，
  `AddEdge` 的环检测为一次 BFS O(N+E)，`DeleteNode` 级联 O(D)；容量检查 O(1)。
- `Reachable`：BFS O(N+E)。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 排序。
- 空间：O(N+E)。
