# replicationgraph

并发安全的内存型有向复制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：`nodes`（`map[string]struct{}`）记录节点存在性；`edges`（`map[Edge]struct{}`）记录边存在性；`adj`（`map[string]map[string]struct{}`）为出边邻接表，供环检测与 `Reachable` 做 DFS。三者始终一致。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集/长度、多余字段），不读状态；然后在写锁内把三份索引浅克隆为候选副本，在其上顺序执行操作，最后才检查节点/边容量。任一步失败直接丢弃候选副本，实现整体回滚；成功则原子替换内部状态并使 `generation` 恰好加一（空批次不变）。
- **所有权**：所有公开方法由一把 `sync.RWMutex` 保护（`Apply` 写锁，`Reachable`/`Snapshot` 读锁）。`Snapshot` 返回新分配的、排序后的切片，调用方对返回值（包括 `Result`/`Snapshot`）的修改不影响内部状态；内部状态从不外泄引用。
- **复杂度**：设批次大小为 B、节点数 V、边数 E。`Apply` 结构校验 O(B·L)（L 为名称长度），克隆 O(V+E)，每个 AddEdge 的环检测为一次 DFS O(V+E)，容量检查 O(1)，整体 O(V+E+B·(V+E))；`Reachable` O(V+E)；`Snapshot` O(V log V + E log E)；空间 O(V+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
