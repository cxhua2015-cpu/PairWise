# topologygraph368

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 实现说明

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。
- 入边不单独建索引；`DeleteNode` 清理入边时扫描邻接表（O(V+E)）。

### 候选事务（candidate transaction）
`Apply` 先做整批结构校验（不读状态），然后在写锁内把 `nodes/edges/out` 深拷贝为候选状态，按序应用全部操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量超限（`ErrCapacity`）即丢弃候选，原状态与 generation 不变。成功时整体换入候选并令 generation 恰好 +1；空批次不增加 generation。容量只在批次末检查，因此批内允许瞬时超容。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写互斥、读读并行。
- `Reachable` 在读锁内对当前一致快照做 DFS。
- `Snapshot` 对节点按字典序、边按 `(From, To)` 稳定排序，返回的切片为新分配内存，与内部状态完全隔离，调用方可自由修改。

### 复杂度
- `Apply`：结构校验 O(批大小)；候选拷贝 O(V+E)；每个 `AddEdge` 环检测 O(V+E)；批次末容量检查 O(1)。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V log V + E log E)（排序）。
- 空间：O(V+E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
