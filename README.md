# controlgraph153

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[edgeKey]struct{}`：边去重与 DeleteEdge O(1)。
- `adj map[string]map[string]struct{}`：出边邻接表，供 Reachable 与环检测做 BFS/DFS。
三者由同一把 `sync.RWMutex` 保护，写操作互斥提交，读操作（Reachable/Snapshot）共享锁。

### 候选事务
`Apply` 先做整批结构校验（kind 合法、字段不多余、名称匹配 `[a-z0-9-_]+` 且不超
`MaxNameBytes`），不读取任何状态；校验通过后在写锁内把当前状态克隆为候选副本，
按顺序应用全部 Op（存在性、环检测等错误即整体放弃），最后才检查最终
节点/边容量，超限返回 `ErrCapacity`。只有全部成功才把候选副本一次性换入并
将 generation 加一——空批次或失败批次不改变 generation，因此不存在部分提交。

### 所有权
- 返回值（`Snapshot` 的切片、`Result`）均为新建副本，调用方修改不影响内部状态。
- 候选副本在 `Apply` 期间为调用 goroutine 私有，提交后所有权移交给 `Graph`，
  失败时直接丢弃，无需回滚日志。
- 包不保留调用方传入的 `Batch`/`Op` 引用。

### 复杂度
设 N=节点数，E=边数，B=批次内操作数：
- `Apply`：结构校验 O(B·名称长度)；克隆 O(N+E)；每个 AddEdge 环检测 O(N+E)，
  DeleteNode 级联删边 O(E)；容量检查 O(1)。
- `Reachable`：BFS，O(N+E)，读锁下使用一致快照。
- `Snapshot`：O(N+E) 拷贝 + O(N log N + E log E) 稳定排序（节点按字典序，
  边按 (From, To) 字典序）。
