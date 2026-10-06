# topologygraph218

并发安全的内存型“控制拓扑图 218”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 用法

```go
g, _ := topologygraph218.New(topologygraph218.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
res, _ := g.Apply(topologygraph218.Batch{Ops: []topologygraph218.Op{
    {Kind: topologygraph218.AddNode, From: "a"},
    {Kind: topologygraph218.AddNode, From: "b"},
    {Kind: topologygraph218.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边稳定排序，切片与内部状态隔离
```

## 设计说明

**索引（所有权）**：`Graph` 独占持有三份索引——`nodes`（节点集合）、`edges`（`Edge` 集合）、`adj`（出边邻接表）。三者互派生但同源更新，任何公开方法返回的切片都是新建副本，调用方无法触碰内部状态。图内始终无环（自环与多步环均在加边时拒绝），因此邻接表始终是 DAG。

**并发**：单把 `sync.RWMutex`。`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，可并发执行且读到一致快照。

**候选事务与回滚**：`Apply` 先在无锁状态下对整个批次做纯结构校验（kind 合法、字段不冗余、名称字符集与长度合法），失败返回 `ErrInvalidInput` 且不触碰状态。随后持锁把批次当作候选事务顺序应用，每步记录逆操作到 undo 日志；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（节点/边数超上限，返回 `ErrCapacity`）失败时，按逆序回放 undo 日志整体回滚，图状态与未调用前完全一致。删除节点时先摘除其全部关联边（同样记入日志）。仅当非空批次成功提交时 generation 递增一次；空批次不改变 generation。

**环检测**：加边 `u→v` 前在候选状态上从 `v` 做 DFS，若可达 `u`（含 `u==v` 自环）则拒绝。

**复杂度**（N=节点数，M=边数，B=批次大小）：
- `Apply`：结构校验 O(B·L)（L 为名称长度）；应用阶段每操作均摊 O(1)，加边环检测最坏 O(N+M)，删节点扫描关联边 O(M)，整体回滚 O(B+M)。
- `Reachable`：O(N+M)。
- `Snapshot`：O(N log N + M log M)，排序保证输出稳定。
- 空间：O(N+M)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
