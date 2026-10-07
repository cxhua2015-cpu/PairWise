# topologygraph383

并发安全的内存型有向无环“控制拓扑图”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

图状态由四份索引组成，全部驻留内存、随事务整体替换：

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：有向边集合（按 `{From, To}` 去重）。
- `out map[string]map[string]struct{}`：出边邻接索引，用于可达性遍历与环检测。
- `in map[string]map[string]struct{}`：入边邻接索引，使 `DeleteNode` 能 O(度数) 清理关联边。

## 候选事务

`Apply` 先在持锁状态下对整个批次做纯结构校验（kind、名称字符集、长度、多余字段），不读取图状态；随后把当前状态**深拷贝为候选状态**，在候选上顺序执行全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`MaxNodes`/`MaxEdges`）失败，直接丢弃候选，原状态不变，实现整体回滚。全部成功才原子替换内部状态并将 `generation` 加一；空批次不增加 generation。

## 所有权

- 所有公开方法通过一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发调用。
- `Snapshot` 返回的 `Nodes`/`Edges` 切片是新分配的副本（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改不影响内部状态。
- `Reachable` 在读锁内基于当前一致快照做 BFS，不会观察到批次中间态。

## 复杂度

设批次含 `k` 个操作，图为 `G = (V, E)`：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(V+E)；每个 `AddEdge` 的环检测为一次 BFS，O(V+E)；容量检查 O(1)。整体 O(V+E + k·(V+E))。
- `DeleteNode`：O(该节点度数)。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V log V + E log E)（排序）。
