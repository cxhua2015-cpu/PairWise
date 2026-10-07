# topologygraph308

并发安全的内存型“控制拓扑图 308”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 实现说明

**索引**
- `nodes: map[string]struct{}` —— 节点存在性 O(1)。
- `edges: map[Edge]struct{}` —— 边存在性 O(1)。
- `out / in: map[string]map[string]struct{}` —— 出/入邻接表，用于可达性遍历与 DeleteNode 级联删边。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集/字节上限、多余字段），不读状态。
- 校验通过后克隆当前状态为候选副本，在副本上顺序执行全部操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本。
- 节点/边容量只在批次末对候选副本检查，超限返回 `ErrCapacity`。
- 全部成功才用副本原子替换当前状态；非空成功批次 `generation` 恰好 +1，空批次不变。

**所有权与并发**
- 单个 `sync.RWMutex` 保护状态与 generation：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，保证读到一致快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），与内部状态完全隔离；调用方修改返回值不影响图。
- 加边前用 DFS 检查 `To → From` 是否已可达（含自环），可达则拒绝，保证图始终是无环的。

**复杂度**（N=节点数，E=边数，B=批内操作数）
- `Apply`：克隆 O(N+E)，每操作均摊 O(1)，AddEdge 环检测 O(N+E)；总计 O(N+E)·O(1) 量级 + B·O(N+E) 最坏。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
