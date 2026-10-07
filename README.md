# topologygraph343

Read `SPEC.md` and implement the package.

## 设计说明

### 索引
图内部维护三个索引：`nodes`（节点名集合）、`edges`（`Edge{From,To}` 集合）以及
`out`（出边邻接表 `map[from]map[to]`）。邻接表使环检测与 `Reachable` 的 DFS 只访问
可达子图；`DeleteNode` 通过 `out` 找到出边，并反向扫描邻接表删除入边。

### 候选事务
`Apply` 先在无（不加锁）对整个批次做结构校验（kind 合法、名称字符集与字节上限、
节点操作不带 `To`），随后持写锁把 `nodes`/`edges`/`out` 拷贝为候选状态，在候选上
顺序应用全部操作。任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量
检查失败（`ErrCapacity`）都直接丢弃候选，原状态不变，实现整体回滚；全部成功才原子
提交并将 `generation` 加一。空批次不修改状态也不增加 generation。

### 所有权
所有公开方法通过一把 `sync.RWMutex` 保护（写操作独占，读操作共享）。`Snapshot`
返回的节点/边切片均为新建并稳定排序（节点按字典序，边按 `(From,To)`），调用方对
返回值的修改不会影响内部状态；`Apply` 不保留入参批次中的任何引用。

### 复杂度
设批次含 k 个操作、图有 V 个节点、E 条边：

- `Apply`：候选拷贝 O(V+E)；每个 AddEdge 的环检测为一次 DFS，O(V+E)；
  DeleteNode 级联删除 O(V+E)。整批 O(k·(V+E))。
- `Reachable`：一次 DFS，O(V+E)。
- `Snapshot`：收集 O(V+E)，排序 O(V log V + E log E)。
- 空间：O(V+E)。
