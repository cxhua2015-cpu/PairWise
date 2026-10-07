# topologygraph303

并发安全的内存型有向控制拓扑图。原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删节点级联删关联边，容量只在批次末检查，失败整体回滚。仅依赖标准库，Go 1.22+。详见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除。
- `adj map[string]map[string]struct{}`：出边邻接表，用于 Reachable 的 BFS 与加边前的反向可达性（环）检查。

## 候选事务

`Apply` 先对整个批次做纯结构校验（kind 合法、字段齐全、名称字符集与长度），不读任何状态；随后在写锁内把当前状态浅拷贝为候选事务（`fork`），按序在候选上应用每个操作。任一步失败或最终节点/边数超容量，直接丢弃候选，原状态零改动（整体回滚）；全部成功才用候选替换内部状态并将 `generation` 加一。空批次不校验状态、不增 generation。

## 所有权

- 所有公开方法通过 `sync.RWMutex` 并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁。
- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方可自由修改，不影响内部状态。
- `Reachable` 在读锁内对当前一致快照做 BFS，不暴露内部引用。

## 复杂度

设 V 为节点数、E 为边数、B 为批次操作数：

- `Apply`：结构校验 O(B)；候选拷贝 O(V+E)；每个 `AddEdge` 的环检查为一次 BFS，O(V+E)；其余操作 O(1) 摊还。整体 O(B·(V+E)) 最坏。
- `Reachable`：一次 BFS，O(V+E)。
- `Snapshot`：O(V log V + E log E) 排序。
- 空间：O(V+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
