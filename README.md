# topologygraph388

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。支持原子批次
（AddNode/DeleteNode/AddEdge/DeleteEdge）、有向环阻止、级联删除、
批次末容量校验与整体回滚、一致快照 Reachable、稳定排序 Snapshot。

## 设计

- **索引**：`nodes` 为节点集合；`edges` 为边集合；`out`/`in` 为正/反
  邻接表（`map[string]map[string]struct{}`）。正邻接用于环检测与
  Reachable 的 BFS，反邻接使 DeleteNode 能 O(度数) 级联删除关联边。
- **候选事务**：`Apply` 先做纯结构校验（不读状态），再在写锁内把
  nodes/edges/out/in 克隆为候选副本，按序应用全部操作（含逐条环检测），
  最后检查节点/边容量。任一失败直接丢弃候选，原状态零改动，实现整体
  回滚；全部成功才一次性换入并令 generation 恰好 +1。空批次不改变
  generation。
- **所有权**：`Graph` 内部状态只由包内代码持有；`Snapshot` 返回新建并
  排序后的切片（节点字典序，边按 (From, To) 字典序），调用方修改返回
  值不影响内部状态。所有公开方法经 `sync.RWMutex` 保护，可并发调用。
- **复杂度**：设批次含 k 个操作、图有 N 个节点、E 条边。候选克隆
  O(N+E)；AddNode/DeleteEdge O(1)；DeleteNode O(度数)；AddEdge 的环
  检测为 O(N+E) 的 BFS；容量检查 O(1)。Apply 总体 O(N+E+k·(N+E))。
  Reachable 为读锁下一次 O(N+E) BFS；Snapshot 为 O(N log N + E log E)
  排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
