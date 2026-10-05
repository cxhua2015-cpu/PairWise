# trustgraph

并发安全的内存型有向信任依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，节点/边容量只在批次末检查，失败整体回滚。

## 使用

```go
g, err := trustgraph.New(trustgraph.Options{MaxNodes: 100, MaxEdges: 500, MaxNameBytes: 32})
res, err := g.Apply(trustgraph.Batch{Ops: []trustgraph.Op{
    {Kind: trustgraph.AddNode, From: "a"},
    {Kind: trustgraph.AddNode, From: "b"},
    {Kind: trustgraph.AddEdge, From: "a", To: "b"},
}})
ok, err := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均稳定排序，切片与内部状态隔离
```

## 设计说明

**索引**：`Graph` 持有四份冗余索引——`nodes` 集合、`edges` 集合（键为
`Edge{From, To}`）、出邻接表 `out[from]` 与入邻接表 `in[to]`。出邻接表服务
可达性 DFS 与环检测；入邻接表让 `DeleteNode` 能 O(关联边数) 级联删除，无需
全图扫描。所有索引在同一临界区内同步维护，永远一致。

**候选事务**：`Apply` 先对整个批次做纯结构校验（kind 合法、字段齐全、名称
字符集与长度），不读任何状态；然后在写锁内把当前状态**深拷贝**为候选状态，
按序在候选上应用全部操作，最后统一检查节点/边容量。任一操作失败或容量超限
时直接丢弃候选，原状态分毫未动，天然实现原子回滚；成功则整体换入候选，
`generation` 恰好 +1（空批次不变）。

**所有权**：`Graph` 从不把内部 map/切片泄露给调用方。`Snapshot` 在读锁内重新
分配并排序切片；`Reachable` 在读锁内基于当前一致快照遍历。读写由
`sync.RWMutex` 保护，所有公开方法可并发调用。

**复杂度**（N=节点数，E=边数，B=批次数，D=关联边数）：

- `Apply`：结构校验 O(B)；候选深拷贝 O(N+E)；`AddNode`/`DeleteEdge` O(1)；
  `DeleteNode` O(D)；`AddEdge` 环检测为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：DFS，O(N+E)。
- `Snapshot`：O(N log N + E log E) 排序，节点按字典序、边按 `(From, To)` 排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
