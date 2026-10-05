# controlgraph183

并发安全的内存型“控制面依赖图 183”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 用法

```go
g, _ := controlgraph183.New(controlgraph183.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(controlgraph183.Batch{Ops: []controlgraph183.Op{
    {Kind: controlgraph183.AddNode, From: "a"},
    {Kind: controlgraph183.AddNode, From: "b"},
    {Kind: controlgraph183.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均稳定排序，切片与内部状态隔离
```

运行示例：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 BFS/DFS 遍历。

**候选事务（candidate transaction）**
`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集/长度、无多余字段），不读取状态；随后加写锁，在四个 overlay 集合（`addedNodes`/`removedNodes`/`addedEdges`/`removedEdges`）上顺序执行各 op。环检测在“已提交状态 + overlay”的候选图上做 DFS。任一步失败直接返回，已提交状态未被触碰，天然整体回滚；节点/边容量只在全部 op 执行完后对最终候选计数检查，超容返回 `ErrCapacity` 并回滚。全部通过才把 overlay 合并进主索引，非空成功批次 `generation` 恰好加一，空批次不变。

**所有权与并发**
- 单个 `sync.RWMutex` 保护全部内部状态：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读操作基于一致快照。
- `Snapshot` 与 `Reachable` 返回的切片/值均为新建副本，调用方修改不影响内部状态。
- 所有公开方法可并发调用，`-race` 下测试通过。

**复杂度**（V 节点、E 边、B 批次大小）
- `Apply`：结构校验 O(B·L)（L 为名称长度）；执行 O(B·(V+E))（环检测 DFS 主导）；提交 O(变更数)。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V log V + E log E)（排序）。
- 空间：O(V+E)。
