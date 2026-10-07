# topologygraph323

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：图状态由三张表组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 哈希集合，O(1) 判重）和 `out`（出边邻接表 `map[string]map[string]struct{}`），供可达性 DFS 与删节点时清理关联边使用。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集/长度、多余字段），不读状态；然后在写锁内把三张表克隆为候选状态，按序在候选上执行全部操作，任一失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败即整体丢弃，实现原子回滚；成功才一次性换入并令 generation 加一。空批次成功但不改变 generation。
- **所有权**：`Snapshot` 在读锁内构造全新切片并排序（节点字典序、边按 `(From,To)` 字典序），返回的切片与内部状态完全隔离；`Apply`/`Reachable` 不泄露内部引用。`Reachable` 在单把读锁内基于当前一致快照做 DFS。
- **复杂度**：设批次含 k 个操作、V 个节点、E 条边。结构校验 O(k·L)（L 为名称长度）；候选克隆 O(V+E)；AddNode/DeleteEdge/AddEdge 判重 O(1)，AddEdge 的环检测为 O(V+E) 的 DFS，DeleteNode 为 O(其关联边数 + 入边扫描 O(V+E))；容量检查 O(1)。`Snapshot` 为 O(V log V + E log E)，`Reachable` 为 O(V+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
