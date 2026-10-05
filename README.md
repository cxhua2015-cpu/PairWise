# controlgraph103

并发安全的内存型控制面依赖图（DAG），仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 用法

```go
g, _ := controlgraph103.New(controlgraph103.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(controlgraph103.Batch{Ops: []controlgraph103.Op{
    {Kind: controlgraph103.AddNode, From: "a"},
    {Kind: controlgraph103.AddNode, From: "b"},
    {Kind: controlgraph103.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
s := g.Snapshot() // 节点与边均稳定排序
```

运行示例：`go run ./cmd/demo`。

## 设计说明

- **索引**：图状态由三张表组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合，O(1) 判重）以及出边邻接表 `out`（`map[string]map[string]struct{}`），供环检测与 `Reachable` 做 DFS。入边不单独建索引，删节点时扫描边集合清理关联边。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集/长度、无多余字段），不触碰状态；然后在写锁内把三张表浅拷贝为候选副本，在副本上顺序应用全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量（节点/边数超过 `Options` 上限）校验失败（`ErrCapacity`）时直接丢弃副本，实现整体回滚；全部成功才一次性换入并提交。非空成功批次 `generation` 恰好加一，空批次不变。
- **所有权**：所有公开方法共用一把 `sync.RWMutex`（`Apply` 写锁，`Reachable`/`Snapshot` 读锁），可任意并发调用。`Snapshot` 返回的切片是全新分配的拷贝，调用方修改不会影响内部状态；`Reachable` 在读锁内对当前一致快照求值。
- **复杂度**：设批次含 k 个操作、图含 V 个节点、E 条边。结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(V+E)；AddNode/DeleteEdge/AddEdge 判重 O(1)；DeleteNode O(E)；每次 AddEdge 的环检测为一次 DFS，O(V+E)；批次末容量检查 O(1)。`Reachable` O(V+E)，`Snapshot` O(V log V + E log E)（排序）。

## 测试

```sh
go test ./...
go test -race ./...
```
