# controlgraph123

并发安全的内存型“控制面依赖图”，仅依赖 Go 标准库（Go 1.22+）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，
节点/边容量仅在批次末检查，任何失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 可比较，直接作键），O(1) 判重。
- `adj map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS，
  以及 `DeleteNode` 时 O(出度) 级联删除出边。入边通过遍历邻接表定位（与图规模成正比）。

## 候选事务（candidate transaction）

`Apply` 先做整批结构校验（kind 合法、名称合法、无多余字段），不读取任何状态；
随后在写锁内把 `nodes`/`edges`/`adj` 克隆为候选副本，按序在副本上应用全部操作，
最后统一检查容量。任一步失败直接丢弃副本返回错误，原状态零改动（天然回滚）；
成功则整体换入副本并将 `generation` 加一。空批次不修改状态也不推进 generation。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁（`sync.RWMutex`）。
- `Snapshot` 与 `Reachable` 在读锁内基于同一份状态构造，返回的是当前一致快照。
- 返回的切片（`Snapshot.Nodes`/`Snapshot.Edges`）均为新建并排序后的副本，调用方修改
  返回值不影响内部状态；内部 map 绝不逃逸到返回值中。
- `Snapshot` 节点按字典序、边按 `(From, To)` 字典序稳定排序。

## 复杂度

设批次大小为 B，节点数 N，边数 E。

- `Apply`：结构校验 O(B·名称长)；候选克隆 O(N+E)；每个 `AddEdge` 环检测为一次 DFS
  O(N+E)；容量检查 O(1)。整体 O(N+E+B·(N+E))，失败时回滚成本为零（丢弃副本）。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
