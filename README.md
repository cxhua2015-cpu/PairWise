# topologygraph393

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重。
- `adj map[string]map[string]struct{}`：出边邻接表，供环检测与
  `Reachable` 的 DFS/BFS 使用，避免每次遍历全量边集。

## 候选事务（candidate transaction）

`Apply` 在写锁内先对整个批次做纯结构校验（kind 合法、名称字符集与
字节上限、节点操作不得携带 `To`），不读取任何状态；随后把
`nodes`/`edges`/`adj` 深拷贝为候选副本，按序在副本上应用全部操作并做
状态检查（`ErrExists`/`ErrNotFound`/`ErrCycle`）。只有全部成功且最终
`len(nodes) <= MaxNodes`、`len(edges) <= MaxEdges` 时，才用候选副本整体
替换正式状态并将 `generation` 加一；任一步失败直接丢弃副本，正式状态
与 generation 均不变（整体回滚）。空批次为无操作，generation 不变。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，
  读写互斥由 `sync.RWMutex` 保证。
- `Reachable` 在读锁内基于当前邻接表遍历，因此观察到的是某一一致快照。
- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片（节点按字典序、边按
  `(From,To)` 稳定排序），调用方修改返回值不影响内部状态；批次中
  `Batch.Ops` 只被读取，不被保留或修改。

## 复杂度

设批次含 k 个操作，图中 V 个节点、E 条边：

- 结构校验：O(k · L)，L 为名称长度上限。
- 候选拷贝：O(V + E)。
- AddNode/DeleteEdge：O(1)；DeleteNode：O(E)（扫描边集级联删除）；
  AddEdge：O(V + E)（DFS 判环）。
- 容量检查：O(1)。整体 `Apply` 为 O(V + E + k·(V + E)) 上界。
- `Reachable`：O(V + E)；`Snapshot`：O(V log V + E log E)（排序）。
- 空间：O(V + E)。
