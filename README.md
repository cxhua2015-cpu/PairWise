# controlgraph113

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 用法

```go
g, _ := controlgraph113.New(controlgraph113.Options{MaxNodes: 64, MaxEdges: 128, MaxNameBytes: 32})
res, _ := g.Apply(controlgraph113.Batch{Ops: []controlgraph113.Op{
    {Kind: controlgraph113.AddNode, From: "a"},
    {Kind: controlgraph113.AddNode, From: "b"},
    {Kind: controlgraph113.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot()
```

## 设计说明

**索引**：图状态由三张表组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合，O(1) 判重）和 `adj`（出边邻接表 `map[from]map[to]`，用于环检测与可达性遍历）。三者始终一致，删除节点时同步清理其出入边。

**候选事务**：`Apply` 先在无锁状态下对整个批次做结构校验（kind 合法、字段不多余、名称字符集与长度合法），再持写锁把当前三张表复制为候选副本，在副本上顺序应用所有操作，最后统一检查节点/边容量。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`/`ErrCapacity`）直接丢弃副本，原状态零改动，实现原子回滚；成功则整体换入候选副本，`generation` 恰好加一（空批次不加）。

**所有权与并发**：所有公开方法经一把 `sync.RWMutex` 保护——`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 与 `Snapshot` 看到的都是某次提交后的完整一致快照。`Snapshot` 返回的节点（字典序）与边（先 From 后 To 字典序）为稳定排序的新建切片，与内部状态完全隔离，调用方可自由修改。

**复杂度**（N 节点、E 边、批次 K 个操作）：
- `New`：O(1)。
- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选复制 O(N+E)；每个 AddEdge 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。整体 O(K·(N+E))。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
