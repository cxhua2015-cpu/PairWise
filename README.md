# topologygraph358

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
AddNode / DeleteNode / AddEdge / DeleteEdge，加边阻止有向环，删除节点级联删除
关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：边集合（去重、存在性 O(1)）。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与删点级联。
- `in map[string]map[string]struct{}`：入边邻接表，使删点级联为 O(度数) 而非全图扫描。

## 候选事务

`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集/长度、节点操作不得
携带 `To`），不读取任何状态；随后在写锁内把当前状态深拷贝为候选事务
`state`，在候选上顺序执行全部操作。任一步失败或最终节点/边数超过容量即
丢弃候选（天然回滚，原状态未被触碰）；全部成功才整体换入并将 generation
加一。空批次为无操作，generation 不变。

## 所有权与并发

- 单把 `sync.RWMutex`：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写互斥、
  读读并发，因此 `Reachable` 看到的始终是一致的当前快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 (From,To) 稳定排序），调用方
  可自由修改，不影响内部状态。
- 换入的候选 `state` 自此归 `Graph` 独占，不再与任何已返回的值共享内存。

## 复杂度

设批次长度 B、节点数 N、边数 E、候选拷贝代价 C = O(N + E)。

- `Apply`：O(C + B·(N + E))（每次 AddEdge 做一次可达性 DFS，O(N + E)）。
- `Reachable`：O(N + E)。
- `Snapshot`：O(N log N + E log E)。
- `New` / 结构校验：O(1) / O(B·名称长度)。
