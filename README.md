# controlgraph118

并发安全的内存型控制面依赖图（见 `SPEC.md`）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 设计说明

**索引**
- `nodes: map[string]struct{}` —— 节点存在性 O(1)。
- `edges: map[Edge]struct{}` —— 边存在性 O(1)。
- `adj: map[string]map[string]struct{}` —— 出边邻接表，供环检测与
  `Reachable` 的 DFS 使用。删除节点时扫描邻接表清理入边。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（不读状态），再加写锁。
- 在克隆出的候选状态上顺序应用全部操作；任一步失败或最终
  节点/边数超容量即丢弃候选，原状态与 generation 不变（整体回滚）。
- 全部成功才原子换入候选状态，generation 恰好 +1；空批次不改动。

**所有权与并发**
- 单个 `sync.RWMutex` 保护状态与 generation：`Apply` 取写锁，
  `Reachable`/`Snapshot` 取读锁，所有公开方法可并发调用。
- `Snapshot` 返回的切片是新分配的排序副本（节点按字典序、边按
  `(From, To)` 稳定排序），调用方修改返回值不影响内部状态。
- `Reachable` 在读锁内基于当前一致快照做遍历。

**复杂度**（N 节点、E 边、批次长度 B）
- 结构校验 O(B · L)，L 为名字长度。
- 候选克隆 O(N + E)；`AddNode`/`DeleteEdge` O(1)；
  `DeleteNode` O(N + E)（扫描入边）；`AddEdge` 环检测 O(N + E)。
- `Reachable` O(N + E)；`Snapshot` O(N log N + E log E)。
- 单批次总复杂度 O(N + E + B · (N + E))，对控制面规模足够。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
