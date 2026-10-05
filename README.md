# controlgraph148

Read `SPEC.md` and implement the package.

本任务要求状态引擎、policy.go 与 coordinator.go 三个生产文件协同实现，详见 SPEC.md。

## 架构

三层结构，各自独立同步、均可并发调用：

- `trustgraph.go`（状态引擎）：`Graph` 用一把 `sync.RWMutex` 保护
  `nodes`/`edges` 两个 map 索引与单调递增的 `generation`。
- `policy.go`（策略层）：`Policy` 用自己的 `sync.RWMutex` 维护 actor 白名单
  （`map[string]struct{}`）与单批操作数上限 `maxOps`；`ReplaceActors` 先完整
  校验新名单，再一次写锁内原子替换整个 map，读者要么看到旧名单要么看到
  新名单，绝不看到中间态。
- `coordinator.go`（协调层）：`Coordinator` 先调用 `Policy.Authorize`
  （拒绝时不触碰核心状态），再委托 `Graph.Apply`，随后用独立互斥锁为
  成功、拒绝、引擎失败三类结果分配连续审计序号并追加 `Decision`。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`，以 `{From, To}` 为键，O(1) 查重与删除。
- 环检测与 `Reachable` 在查询时按需从边集构建邻接表做 DFS，不维护
  额外的传递闭包索引，以换取写入路径的简单与正确性。

## 候选事务（candidate transaction）

`Apply` 先在无锁情况下对批次做完整结构校验（kind、名称字符集与字节
上限、多余字段），不读取任何状态。随后持写锁克隆 nodes/edges 得到候选
状态，在副本上顺序执行全部操作；任一步失败（ErrExists/ErrNotFound/
ErrCycle）或批次末容量检查（ErrCapacity）失败都直接丢弃副本，原状态
不变，实现整体回滚。只有全部成功才把副本一次性交换进引擎并将
`generation` 加一（空批次不增加）。

## 所有权

- `Snapshot` 返回新建并稳定排序的 `Nodes`/`Edges` 切片，与内部 map 完全
  隔离，调用方可自由修改。
- `Coordinator.Decisions` 每次返回审计日志的独立副本，不别名内部存储。
- `Policy.ReplaceActors` 不保留调用方切片，内部重建 set。

## 复杂度

- `Apply`：结构校验 O(B·L)；候选克隆 O(N+E)；执行 O(B·(N+E)) 上界
  （DeleteNode 的关联边清理与 AddEdge 的环检测各为 O(N+E)）；容量检查
  O(1)。B 为批内操作数，L 为名称长度，N/E 为节点/边数。
- `Reachable`：O(N+E)，读锁下的一致快照。
- `Snapshot`：O(N log N + E log E)，排序后返回。
- `Authorize`/`ReplaceActors`：O(1) / O(A)，A 为 actor 数。
- `Coordinator.Apply`：授权 + 引擎调用 + O(1) 审计追加。
