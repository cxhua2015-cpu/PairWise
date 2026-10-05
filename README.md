# controlgraph088

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量在批次末检查、失败整体回滚。详见 `SPEC.md`。

## 设计说明

- **索引**：节点存于 `map[string]struct{}`，边存于 `map[Edge]struct{}`
  （`Edge{From, To}` 为可比较键）。存在性判断与去重均为 O(1) 均摊。
  `Snapshot` 时对键做稳定排序（节点按字典序，边按 `(From, To)`），
  返回新分配的切片，与内部状态完全隔离。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与
  字节上限、多余字段），不读取任何状态；随后在互斥锁内把节点/边映射
  复制为候选副本，按顺序在副本上应用全部操作并做语义检查
  （`ErrExists` / `ErrNotFound` / `ErrCycle`），最后检查最终容量
  （`ErrCapacity`）。任一失败直接丢弃副本，原状态零改动，实现整体回滚；
  成功则原子交换映射指针并将 `generation` 加一（空批次不增加）。
- **所有权**：所有公开方法共用一把 `sync.Mutex`，`Apply` 持锁期间完成
  候选应用与交换，`Reachable` / `Snapshot` 在同一锁内读取，因此读到的是
  一致快照。返回的 `Result`、`Snapshot` 及其切片均为新分配对象，调用方
  可自由修改而不影响图。锁内无 I/O、无回调，无死锁风险。
- **复杂度**：设批次含 k 个操作、图中 N 个节点、E 条边。结构校验 O(k·L)
  （L 为名称长度）；候选复制 O(N+E)；`AddNode`/`DeleteEdge` O(1) 均摊，
  `DeleteNode` O(E)（级联扫描），`AddEdge` 的环检测为从 `To` 到 `From`
  的 DFS，O(N+E)；容量检查 O(1)。单批次总计 O(k·(N+E))。
  `Reachable` 为一次 DFS，O(N+E)；`Snapshot` 为 O(N log N + E log E)。
