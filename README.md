# controlgraph193

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除。
- `adj map[string]map[string]struct{}`：正向邻接表，用于环检测与 `Reachable` 的 DFS/BFS。

### 候选事务（原子批次）
`Apply` 在写锁内先做全量结构校验（kind、名称字符集与字节上限、多余字段），再克隆三份索引为候选状态，按序应用全部操作（存在性、环检测即时检查），最后才校验节点/边容量。任一步失败直接丢弃候选状态，原图不变；成功则整体换入并将 `generation` 加一。空批次不修改状态也不推进 generation。

### 所有权与并发
- 所有公开方法经由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发调用。
- `Snapshot` 返回的切片是新分配的副本，调用方修改不影响内部状态；内部 map 也绝不逃逸到返回值中。
- 环检测与可达性复用同一 DFS：加边 `u->v` 前检查候选图中 `v` 是否已可达 `u`。

### 复杂度
- `Apply`：结构校验 O(Σ|op|)；克隆 O(N+E)；每条 `AddEdge` 环检测 O(N+E)；`DeleteNode` 级联删除 O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)，节点按字典序、边按 (From, To) 稳定排序。
