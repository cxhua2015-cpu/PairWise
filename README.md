# ownershipgraph

并发安全的内存型所有权依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
图状态由三份内存索引组成，全部在单个 `sync.RWMutex` 保护下：
- `nodes: map[string]struct{}` —— 节点存在性，O(1) 查询。
- `out: map[string]map[string]struct{}` —— 出边邻接（from → to 集合），用于可达性 DFS 与环检测。
- `in:  map[string]map[string]struct{}` —— 入边邻接（to → from 集合），用于 `DeleteNode` 时 O(关联边数) 级联删除关联边。

边数单独计数，容量检查为 O(1)。

### 候选事务（原子批次）
`Apply` 分三个阶段：
1. **结构校验**：先对全部 op 做纯结构校验（kind 合法、无额外字段、名称字符与长度），不读取任何图状态；任一失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把 `nodes/out/in` 深拷贝为候选状态，按顺序应用全部 op（`ErrExists`/`ErrNotFound`/`ErrCycle` 即失败）。加边前用 DFS 检查 `to` 是否已可达 `from`，从而阻止有向环（含自环）。
3. **容量检查与提交**：仅在批次末对最终候选状态检查 `MaxNodes`/`MaxEdges`，超限返回 `ErrCapacity`。任何失败直接丢弃候选状态，原图零改动（整体回滚）；成功则整体换入候选状态并将 `generation` 加一。空批次不校验容量、不增加 generation。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，二者读取的始终是某一已提交 generation 的一致快照。
- `Snapshot` 返回的节点（字典序）与边（按 `(From, To)` 排序）切片为新分配的副本，调用方修改不会影响内部状态；`Apply` 成功后旧状态整体被替换，不存在共享可变结构。
- 失败批次不改变 generation；非空成功批次 generation 恰好加一。

### 复杂度
设批次含 k 个 op，图含 N 个节点、E 条边：
- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(N+E)；每个 `AddEdge` 环检测 O(N+E)；容量检查 O(1)。整体 O(N + E + k·(N+E))，失败时回滚为 O(1)（丢弃候选）。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)。
- `New`：O(1)。空间 O(N+E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
