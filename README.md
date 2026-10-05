# workflowgraph

并发安全的内存型工作流依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重判断。
- `out / in map[string]map[string]struct{}`：出边/入边邻接索引，用于可达性遍历、环检测，以及删除节点时 O(度数) 级联清理关联边。

### 候选事务（原子批次）
`Apply` 在写锁内执行：先对全部 op 做纯结构校验（kind、名称字符集与字节上限、多余字段），不读取图状态；随后顺序应用并记录 undo 日志（AddNode↔删节点、DeleteNode↔恢复节点及其级联删除的边、AddEdge↔删边、DeleteEdge↔插边）。任何 op 失败或批次末容量（MaxNodes/MaxEdges）超限，即按逆序回放 undo 日志整体回滚，返回对应错误且 generation 不变。空批次为 no-op，不推进 generation；非空成功批次 generation 恰好 +1。

### 所有权与并发
所有公开方法经由单个 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发调用。`Snapshot` 返回新建并排序（节点字典序；边按 From、再 To 稳定排序）的切片，`Reachable` 在读锁内基于当前一致快照做 DFS，返回值均不与内部状态共享内存，调用方可自由修改。

### 复杂度
- AddNode / DeleteNode：O(1) / O(deg(n))。
- AddEdge / DeleteEdge：O(V+E)（DFS 环检测）/ O(1)。
- Apply：O(Σ 各 op 复杂度)，回滚代价与已应用 op 数同阶。
- Reachable：O(V+E)。Snapshot：O(V log V + E log E)。
- 空间：O(V+E)。
