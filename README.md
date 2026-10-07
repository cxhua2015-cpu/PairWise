# topologygraph378

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除判断。
- `out / in map[string]map[string]struct{}`：出边/入边邻接索引。`out` 用于可达性 DFS，`in` 用于 DeleteNode 时 O(度数) 摘除全部关联边。

### 候选事务
`Apply` 分两阶段：先对整个批次做纯结构校验（kind、名称字符集与字节上限、多余字段），不读任何状态；再持写锁就地顺序执行，每个成功的操作记录一条逆向 undo（AddNode↔删节点、DeleteNode↔恢复节点及其被摘除的关联边、AddEdge↔删边、DeleteEdge↔加边）。任一操作失败或批次末最终节点/边数超容量时，按逆序回放 undo 整体回滚，状态与 generation 保持不变。非空成功批次 generation 恰好 +1，空批次不变。

### 所有权与并发
所有公开方法通过一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 Reachable 总是基于某一一致快照。`Snapshot` 返回新分配的切片（节点按字典序、边按 (From, To) 稳定排序），调用方对返回值的修改不影响内部状态。

### 复杂度
- 结构校验：O(批次操作数 × 名称长度)。
- AddNode/DeleteEdge：O(1)；DeleteNode：O(该节点度数)。
- AddEdge 的成环检查与 Reachable：O(V+E) DFS。
- 批次末容量检查：O(1)；回滚代价与已执行操作数成正比。
- Snapshot：O(V log V + E log E) 排序。
