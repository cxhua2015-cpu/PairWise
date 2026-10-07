# topologygraph363

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 使用

```go
g, _ := topologygraph363.New(topologygraph363.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(topologygraph363.Batch{Ops: []topologygraph363.Op{
    {Kind: topologygraph363.AddNode, From: "a"},
    {Kind: topologygraph363.AddNode, From: "b"},
    {Kind: topologygraph363.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均稳定排序
```

运行演示：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。

## 设计说明

**索引**。图内部维护三份索引：`nodes map[string]struct{}`（节点存在性
O(1)）、`edges map[Edge]struct{}`（边存在性 O(1)）、
`out map[string]map[string]struct{}`（出边邻接表，用于可达性遍历与环检测）。
入边不单独建索引，删除节点时扫描边集级联删除。

**候选事务**。`Apply` 分两阶段：先在无锁状态下做完整结构校验（未知
kind、非法名称、多余字段一律 `ErrInvalidInput`，不读取任何图状态）；随后
在写锁内把当前状态复制为候选副本（candidate），在副本上顺序应用全部操作
——存在性/缺失冲突返回 `ErrExists`/`ErrNotFound`，加边前用候选图上的
DFS 检测 `to ⇝ from` 可达性以阻止环（`ErrCycle`）。所有操作应用完后才
检查最终节点/边容量（`ErrCapacity`）。任一步失败直接丢弃候选副本，实现
整体回滚；成功时一次性换入候选状态并将 generation 加一（空批次不变）。

**所有权与并发**。所有公开方法通过 `sync.RWMutex` 保护：`Apply` 持写锁，
`Reachable`/`Snapshot` 持读锁，可并发调用。`Snapshot` 返回的节点与边切片
均为新建并排序（节点按字典序、边按 `(From, To)` 字典序），调用方修改返回
切片不影响内部状态；`Reachable` 在读锁内基于当前一致快照做 BFS/DFS。

**复杂度**。设批次含 k 个操作、图有 V 个节点、E 条边：
- `New`：O(1)。
- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选复制 O(V+E)；
  `AddNode`/`DeleteEdge` O(1)，`DeleteNode` O(E)（级联扫描），
  `AddEdge` 环检测 O(V+E)；容量检查 O(1)。整体 O(k·(V+E))。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V log V + E log E)（排序）。
