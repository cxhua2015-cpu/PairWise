# controlgraph198

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除判断。
- `out / in map[string]map[string]struct{}`：出边/入边邻接表，用于 DeleteNode 级联删边（O(度数)）和环检测 / Reachable 的 DFS。

**候选事务（原子批次）**
- `Apply` 先做纯结构校验（kind、名称字符集与字节上限、多余字段），不读任何状态，失败返回 `ErrInvalidInput`。
- 随后在写锁内把每个 op 直接应用到图上，同时记录**逆操作**（undo 日志）：AddNode↔DeleteNode、AddEdge↔DeleteEdge；DeleteNode 先级联删除关联边，并把“重新加节点 + 重新加边”记入日志。
- 任一 op 失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败（`ErrCapacity`），按逆序回放 undo 日志，整体回滚，generation 不变。
- 容量（MaxNodes/MaxEdges）只在批次末尾检查，因此批内“先删后增”可以成功。
- 非空成功批次 generation 恰好 +1；空批次成功但 generation 不变。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁（`sync.RWMutex`），因此 Reachable 与 Snapshot 总是观察到批次粒度的一致快照。
- `Snapshot` 返回的切片是新分配的副本，节点按字典序、边按 (From, To) 稳定排序；调用方修改返回值不影响内部状态。
- 名称校验（非空、`[a-z0-9-_]`、字节上限）在加锁前完成，不持有锁。

**复杂度**（V=节点数，E=边数，B=批内 op 数）
- AddNode/DeleteNode/AddEdge/DeleteEdge：O(1)，DeleteNode 另加 O(度数) 级联。
- AddEdge 环检测与 Reachable：一次 DFS，O(V+E)。
- Apply：O(B·(V+E)) 上界（每个 AddEdge 一次 DFS），回滚 O(B + 级联边数)。
- Snapshot：O(V log V + E log E) 排序；空间 O(V+E)。
