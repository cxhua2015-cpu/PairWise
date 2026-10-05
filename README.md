# controlgraph173

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与
  删除节点时的出边级联。
- `in map[string]map[string]struct{}`：入边邻接表，用于删除节点时的
  入边级联。

四个索引始终一致地同步更新。

## 候选事务（candidate transaction）

`Apply` 先在持锁状态下把当前四个索引浅拷贝为候选事务 `txn`，所有操作
按序作用于副本；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或
批次末容量检查（`ErrCapacity`）失败时直接丢弃副本，图状态与
generation 完全不变。只有全部成功才把副本整体换入并将 generation 加一。
空批次不改变 generation。结构校验（未知 kind、多余字段、非法名称）在
读取任何状态之前完成，返回 `ErrInvalidInput`。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁。
- `Reachable` 在读锁内基于当前一致快照做 DFS，不复制图。
- `Snapshot` 返回新建并排序（节点字典序、边按 `(From, To)` 字典序）的
  切片，调用方对返回值的修改不影响内部状态。
- 候选事务的副本只在提交时成为共享状态，之后不再被写，满足
  “不可变共享、可变独占”的所有权纪律。

## 复杂度

设批次含 B 个操作，图有 N 个节点、E 条边：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；
  每个 AddNode/DeleteEdge O(1)，DeleteNode O(度数)，AddEdge 的环检测
  为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
