# controlgraph128

并发安全的内存型有向依赖图，仅依赖标准库（Go 1.22+）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}`：正向邻接表，用于环检测与
  `Reachable` 的 BFS，也用于 `DeleteNode` 时 O(出度) 级联删除出边。

## 候选事务（candidate transaction）

`Apply` 先对整个批次做纯结构校验（kind 合法、字段不多不少、名称符合
`[a-z0-9-_]` 且不超过 `MaxNameBytes`），不读取任何图状态。随后在写锁内
把 `nodes`/`edges`/`out` 深拷贝为候选状态，按序在候选上应用每个操作；
任一操作违反 `ErrExists` / `ErrNotFound` / `ErrCycle` 即丢弃候选直接返回。
全部成功后才对候选做最终的 `MaxNodes` / `MaxEdges` 检查，超限返回
`ErrCapacity` 并丢弃候选。只有完全成功的非空批次才会把候选整体换入并令
`generation` 恰好加一；空批次不修改状态也不增加 generation。

## 所有权与并发

- 所有公开方法通过一把 `sync.RWMutex` 保护：`Apply` 持写锁，
  `Reachable` / `Snapshot` 持读锁，可并发调用。
- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片，节点按字典序、边按
  `(From, To)` 稳定排序；调用方修改返回值不影响内部状态。
- `Reachable` 在读锁内对当前一致快照做 BFS，不会观察到批次中间态。

## 复杂度

设批次大小为 B，节点数 V，边数 E：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(V+E)；
  每个 `AddEdge` 的环检测为一次 BFS，O(V+E)；整体 O(B·(V+E))。
- `DeleteNode`：O(关联边数)，出边经邻接表直达，入边需扫描邻接表 O(V)。
- `Reachable`：O(V+E) BFS。
- `Snapshot`：O(V log V + E log E) 排序。
- 空间：O(V+E)。
