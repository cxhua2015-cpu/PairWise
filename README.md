# topologygraph308

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

内部状态 `state` 持有四类索引：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边去重与存在性，O(1) 查询。
- `adj map[string]map[string]struct{}`：正向邻接表，用于可达性 BFS 与删点时的出边级联。
- `rev map[string]map[string]struct{}`：反向邻接表，用于删点时的入边级联。

所有索引在同一写锁内同步维护，不会彼此失步。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在不读取任何状态的情况下校验全部 op 的 kind、名称字符集
   （非空 ASCII 小写字母/数字/`-`/`_`，长度 ≤ `MaxNameBytes`）与多余字段，
   任一失败返回 `ErrInvalidInput`。
2. **候选执行**：克隆当前状态得到候选副本，在副本上按序应用 op
   （存在性、重复、环检测等错误立即返回）；全部成功后仅在末尾检查
   节点/边容量，超限返回 `ErrCapacity`。任何失败都直接丢弃候选副本，
   原状态零改动，实现整体回滚。成功则以候选副本原子替换当前状态，
   非空批次 generation 恰好加一，空批次不变。

## 所有权与并发

- `Graph` 内嵌 `sync.RWMutex`：`Apply` 持写锁，`Reachable` / `Snapshot` 持读锁，
  因此 `Reachable` 观察到的始终是某个已提交批次的完整一致快照。
- `Snapshot` 返回新建并对节点、边（按 From 再按 To）稳定排序的切片，
  调用方对返回值的任何修改都不影响内部状态；`Apply` 成功后旧状态整体被替换，
  不存在与调用方共享的可变内存。
- 校验中的名称与 op 均按值处理，不保留调用方切片引用。

## 复杂度

设 N 为节点数、E 为边数、K 为批次 op 数：

- `New`：O(1)。
- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选克隆 O(N+E)；
  每个 `AddEdge` 的环检测为一次 BFS，O(N+E)；末尾容量检查 O(1)。
  总体 O(K·(N+E))。
- `Reachable`：一次 BFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)。
- `DeleteNode`：O(关联边数)。
