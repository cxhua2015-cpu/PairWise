# failovergraph

并发安全的内存型故障转移有向图（DAG）。原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。仅依赖标准库，需 Go 1.22+。语义详见 `SPEC.md`。

## 设计说明

- **索引**：状态由两个哈希映射组成——`nodes map[string]struct{}` 与 `edges map[Edge]struct{}`，节点存在性、边存在性、删除均为 O(1) 均摊。未维护持久邻接表；可达性通过在边集上按需 BFS 完成，以换取极简的写路径与回滚语义。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限、字段规则），不触碰状态；随后在写锁内把当前状态 `clone` 为候选副本，顺序应用全部操作（含逐边环检测），最后才检查节点/边容量。任何一步失败直接丢弃候选，原状态零改动，天然实现原子回滚；成功则以指针替换提交，generation 恰好 +1（空批次不变）。
- **所有权与并发**：`Graph` 内嵌 `sync.RWMutex`。`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此读操作总看到某一次已提交批次的一致快照。所有返回的切片（`Snapshot.Nodes`、`Snapshot.Edges`）都是新分配的副本，调用方修改不影响内部状态；内部 map 在提交后永不原地修改（写路径只改克隆体），读路径无需拷贝即可安全遍历。
- **复杂度**：设 N 为节点数、E 为边数、B 为批次操作数。`Apply` 为 O(N + E + B·E)（克隆 O(N+E)，每次 `AddEdge` 的环检测 BFS 为 O(E)，末态容量检查 O(1)）；`Reachable` 为 O(E)；`Snapshot` 为 O(N log N + E log E)（稳定排序：节点按字典序，边按 `(From, To)` 字典序）。空间 O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
