# topologygraph363

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 节点集合：`map[string]struct{}`，O(1) 存在性判断。
- 边集合：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 去重与删除。
- 未维护邻接表；可达性通过边集扫描 DFS 完成，以换取极简的候选事务复制逻辑。

### 候选事务（原子批次）
`Apply` 分两阶段：
1. **结构校验**：在读取任何状态前校验全部 op 的 kind、名称字符集（`[a-z0-9-_]`、非空、≤ `MaxNameBytes`）及多余字段，失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内克隆节点/边两个 map 作为候选状态，顺序应用全部 op（存在性、环检测等错误立即返回）；最后才检查节点/边容量（`ErrCapacity`）。任何失败直接丢弃候选 map，原状态零成本回滚；成功则整体换入并将 `generation` 加一。空批次不改变 generation。

### 所有权与并发
- 所有公开方法由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发调用。
- `Snapshot` 返回的切片是新分配的副本，调用方修改不会影响内部状态；图状态只能经 `Apply` 改变。
- 加边前用候选边集做 DFS 检查 `To ⇝ From` 是否已可达（含自环），可达则返回 `ErrCycle`，保证图始终无环。

### 复杂度
设批次含 B 个 op，图中 N 个节点、E 条边：
- `Apply`：克隆 O(N+E)；每个 AddEdge 的环检测 O(E)；末尾容量检查 O(1)。整体 O(N + E + B·E)。
- `Reachable`：O(E) DFS，基于读锁下的一致快照。
- `Snapshot`：O(N log N + E log E)，节点按字典序、边按 (From, To) 稳定排序。
- 空间：O(N + E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
