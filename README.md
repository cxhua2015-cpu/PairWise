# failovergraph

并发安全的内存型有向“故障转移图”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 使用

```go
g, _ := failovergraph.New(failovergraph.Options{MaxNodes: 64, MaxEdges: 256, MaxNameBytes: 32})
r, _ := g.Apply(failovergraph.Batch{Ops: []failovergraph.Op{
    {Kind: failovergraph.AddNode, From: "a"},
    {Kind: failovergraph.AddNode, From: "b"},
    {Kind: failovergraph.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均稳定排序
```

## 设计说明

- **索引**：图状态由三张表组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 哈希集合，O(1) 判重）、`out`（出边邻接表 `map[From]map[To]`，用于环检测与可达性遍历）。入边不单独建索引，删除节点时扫描出边表清理入边。
- **候选事务**：`Apply` 分三个阶段。阶段一仅做结构校验（kind 合法、字段形状、名称字符集与长度），不读状态；阶段二把 `nodes/edges/out` 深拷贝为候选副本，按序应用全部操作（存在性、未知节点、环检测等错误立即返回）；阶段三只在候选最终状态上检查节点/边容量。任一阶段失败即丢弃候选，原状态完全不变（整体回滚）；成功则一次性替换内部状态并将 `generation` 加一。空批次为成功无操作，不增加 generation。
- **所有权**：所有公开方法通过 `sync.RWMutex` 保护（`Apply` 写锁，`Reachable`/`Snapshot` 读锁）。`Snapshot` 返回新建并排序的切片，`Apply` 提交时整体替换 map 而非原地修改，因此返回值与内部状态完全隔离，调用方修改返回切片不影响图。
- **复杂度**：设批次含 k 个操作，节点数 n、边数 m。结构校验 O(k·L)（L 为名称长度上限）；候选拷贝 O(n+m)；AddEdge 的环检测为一次 DFS，O(n+m)；DeleteNode 清理入边 O(n+m)；容量检查 O(1)。整批 Apply 为 O(n+m+k·(n+m)) 上界，Reachable 为 O(n+m)，Snapshot 为 O(n log n + m log m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
