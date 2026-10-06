# topologygraph213

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- 不维护邻接表；可达性通过在边集合上按需 DFS 完成，以内存换取实现简单性。
  `DeleteNode` 级联扫描边集合一次完成。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在不读取任何状态下校验整个批次（kind 合法、节点操作不带
   `To`、名称非空且仅含 `[a-z0-9-_]` 且不超过 `MaxNameBytes`），任一失败返回
   `ErrInvalidInput`。
2. **候选执行**：在持写锁期间把 `nodes`/`edges` 复制为候选副本，按顺序在副本上
   应用每个操作（存在性、未找到、环检测等错误立即返回）；最后才检查
   `len(nodes) <= MaxNodes` 与 `len(edges) <= MaxEdges`，超限返回 `ErrCapacity`。
   任何失败直接丢弃候选副本即完成回滚；成功则整体换入并将 `generation` 加一。
   空批次为无操作，不增加 `generation`。

环检测：新增 `From->To` 前在候选边集上检查 `To` 是否已可达 `From`
（含 `From == To` 自环），可达则返回 `ErrCycle`。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，
  因此 `Reachable` 总是基于当前一致快照。
- `Snapshot` 返回的 `Nodes`/`Edges` 为新分配的切片（节点按字典序、边按
  `(From,To)` 稳定排序），调用方修改返回值不影响内部状态。
- 名称上限等配置在 `New` 时校验，三项上限必须为正，否则 `ErrInvalidOptions`。

## 复杂度

设批次含 `k` 个操作，图中 `n` 个节点、`m` 条边：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选复制 O(n+m)；每个 `AddEdge`
  的环检测 O(n+m)，其余操作 O(1) 均摊；`DeleteNode` 级联 O(m)。总计
  O(n + m + k·(n+m))，空间 O(n+m)。
- `Reachable`：O(n+m) DFS。
- `Snapshot`：O(n log n + m log m) 排序，O(n+m) 空间。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
