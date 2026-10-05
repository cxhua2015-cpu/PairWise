# controlgraph108

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 用法

```go
g, _ := controlgraph108.New(controlgraph108.Options{MaxNodes: 64, MaxEdges: 128, MaxNameBytes: 32})
r, _ := g.Apply(controlgraph108.Batch{Ops: []controlgraph108.Op{
    {Kind: controlgraph108.AddNode, From: "a"},
    {Kind: controlgraph108.AddNode, From: "b"},
    {Kind: controlgraph108.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均按字典序稳定排序
```

## 设计说明

**索引**。图以三张哈希表持有：`nodes`（节点集合）、`out`（from → to 集合
的邻接表）、`in`（to → from 集合的反向邻接表）。反向索引使 `DeleteNode`
的级联删边与 `DeleteEdge` 都是 O(度数) 而非全图扫描；边数由候选事务在
增删时增量维护，容量检查为 O(1)。

**候选事务**。`Apply` 先对整个批次做纯结构校验（kind 合法、节点操作无
多余字段、名称字符集与长度），不读取任何状态；随后在写锁内把当前状态
深拷贝为候选副本，按顺序在副本上应用全部操作（环检测在副本的 `out`
索引上做 DFS），最后才检查节点/边容量。任一步失败直接丢弃副本，原图
不变，天然实现整体回滚；全部成功则以指针交换方式一次性提交，非空批次
`generation` 恰好加一，空批次不变。

**所有权**。所有公开方法均可并发调用：`Apply` 取写锁，`Reachable` 与
`Snapshot` 取读锁，因此 `Reachable` 总是基于某一已提交代的一致快照。
`Snapshot` 返回的切片是新分配的拷贝，调用方修改返回的 `Nodes`/`Edges`
不影响内部状态；提交后候选副本归图所有，不再被复用。

**复杂度**。设批次含 k 个操作、图为 N 节点 E 边：结构校验 O(k)；候选
拷贝 O(N+E)；每个 `AddEdge` 的环检测为一次 DFS，O(N+E)；`DeleteNode`
为 O(度数)；容量检查 O(1)。`Reachable` 为一次 DFS，O(N+E)；`Snapshot`
为 O(N log N + E log E)（排序）。空批次只读锁读取代际号，O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
