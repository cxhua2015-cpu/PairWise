# topologygraph413

并发安全的内存型“控制拓扑图 413”（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除
关联边，容量只在批次末检查、失败整体回滚。

## 架构与索引

- `trustgraph.go`：核心事务引擎。`Graph` 用一把 `sync.RWMutex` 保护全部状态；
  写事务（`Apply`）持写锁，只读方法（`Reachable`/`Snapshot`/`Stats`/`Clone`）持读锁。
- 索引：`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）、`out` / `in`
  （出/入邻接表）。`out` 支撑环检测与可达性 BFS，`in` 支撑 `DeleteNode` 的级联删除。
- `validation.go`：`ValidateBatch` 做纯结构预检（kind 合法、节点操作无多余 `To`、
  名字为非空 `[a-z0-9_-]` 且不超 `MaxNameBytes`、禁止自环），不读写图状态；
  `Apply` 复用同一预检，保证两条入口语义一致。
- `stats.go`：`Stats` 在读锁下一次性读取 generation/节点数/边数，满足线性一致。
- `clone.go`：`Clone` 在读锁下深拷贝全部索引与逻辑时钟（generation），与原图零共享。

## 候选事务与回滚

`Apply` 先做结构预检，再在写锁内把当前状态复制为 `candidate`，按序应用全部操作
（存在性冲突 `ErrExists`、缺失 `ErrNotFound`、环 `ErrCycle` 即时失败）。仅当所有操作
成功且最终 `len(nodes) <= MaxNodes`、`len(edges) <= MaxEdges` 时才把 candidate 整体换入
并将 generation 加一；任一步失败直接丢弃 candidate，原状态与 generation 不变，实现
原子回滚。空批次成功且不推进 generation。

## 所有权

所有公开方法返回的切片/结构（`Snapshot`、`Clone`）都是新建对象，与内部 map 无任何
共享；调用方修改返回值不影响图。`Clone` 的副本同样完全独立，两个图后续互不影响。

## 复杂度

- `AddNode`/`DeleteEdge`：O(1)；`DeleteNode`：O(度数)。
- `AddEdge`：环检测为从 `To` 到 `From` 的 BFS，O(V+E)。
- `Apply`：候选复制 O(V+E) + 各操作开销；容量检查 O(1)。
- `Reachable`：BFS O(V+E)；`Snapshot`：排序 O(V log V + E log E)。
- `Clone`：O(V+E)；`Stats`/`ValidateBatch`：O(1)/O(批次数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
