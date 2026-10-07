# topologygraph348

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `adj map[string]map[string]struct{}`：出边邻接表，用于环检测与
  `Reachable` 的 DFS。边集与邻接表始终同步维护。

## 候选事务（candidate transaction）

`Apply` 分三个阶段：

1. **结构校验**：先对整个批次做纯结构检查（kind 合法、节点操作不带
   多余字段、名称非空且仅含 `[a-z0-9-_]` 且不超过 `MaxNameBytes`），
   不读取任何图状态；任何违规返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把当前状态浅拷贝为候选副本（`fork`），按序
   应用全部操作。环检测在候选邻接表上做 DFS：加入 `from→to` 前若
   `to` 可达 `from`（含自环）则返回 `ErrCycle`。
3. **提交**：仅在批次末检查 `len(nodes) ≤ MaxNodes` 与
   `len(edges) ≤ MaxEdges`，超限返回 `ErrCapacity`。任一阶段失败直接
   丢弃候选副本即完成回滚；成功则整体替换内部状态并将 `generation`
   加一。空批次不校验状态、不推进 generation。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable` / `Snapshot`
  持读锁，读写互斥，因此 `Reachable` 总是基于某一已提交批次后的
  一致快照。
- `Snapshot` 返回新分配的切片，节点按字典序、边按 `(From, To)`
  稳定排序；调用方修改返回值不影响内部状态。
- 候选副本只在单个 `Apply` 内使用，绝不跨调用共享。

## 复杂度

设 `N` 为节点数、`E` 为边数、`B` 为批次操作数：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；
  每次 `AddEdge` 环检测 O(N+E)；容量检查 O(1)。
- `DeleteNode`：O(E)（扫描边集做级联删除）。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
