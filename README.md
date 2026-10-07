# topologygraph373

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge，加边阻止有向环，删节点级联删边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：边集合，键即 `(From, To)`，O(1) 判重/删除。
- `out / in map[string]map[string]struct{}`：出/入邻接索引，供 `Reachable` BFS 使用；在每次成功提交时由 `edges` 重建，保证与主状态一致。

## 候选事务（candidate transaction）

`Apply` 先在 `sync.RWMutex` 写锁内做**纯结构校验**（kind 合法、名称非空且仅含 `[a-z0-9-_]`、不超 `MaxNameBytes`、节点操作不得带 `To`），不读状态。随后在 `nodes`/`edges` 的**拷贝**上按序重放所有操作（ErrExists/ErrNotFound/ErrCycle 即时失败并丢弃拷贝）；只有全部成功且**批次末** `len(nodes)<=MaxNodes && len(edges)<=MaxEdges` 才整体提交，否则原状态不变（回滚）。非空成功批次 `generation` 恰好 +1；空批次与失败批次不变。

## 所有权

- 所有公开方法（`New` 除外）可并发调用：写操作持写锁，`Reachable`/`Snapshot` 持读锁。
- `Snapshot` 返回的 `Nodes`/`Edges` 为新分配切片（节点按字典序、边按 `(From,To)` 稳定排序），调用方可自由修改，与内部状态完全隔离。
- 名称合法性在读取 `opts` 时无需锁：`Options` 在 `New` 时校验（三项上限必须为正）后只读。

## 复杂度

设 V=节点数，E=边数，B=批内操作数：

- `Apply`：校验 O(B·L)（L 为名称长度）；拷贝 O(V+E)；重放每操作 O(1)，其中 AddEdge 的环检测为一次 DFS，O(V+E)；DeleteNode 级联扫描 O(E)。整体 O(V+E+B·(V+E))。
- `Reachable`：BFS，O(V+E)。
- `Snapshot`：收集 O(V+E)，排序 O(V log V + E log E)。
- 空间：O(V+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
