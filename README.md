# controlgraph143

Read `SPEC.md` and implement the package.

本任务要求状态引擎、policy.go 与 coordinator.go 三个生产文件协同实现，详见 SPEC.md。

## 架构

- `trustgraph.go`：状态引擎。持有节点/边事务数据、generation 与快照。
- `policy.go`：准入策略。独立同步的 actor 白名单（整体原子替换）与单批操作数上限。
- `coordinator.go`：协调层。先 `Authorize` 再委托状态引擎，并为成功、拒绝、引擎失败记录连续审计序号。

## 索引

状态引擎使用两个哈希索引：`nodes map[string]struct{}` 与 `edges map[Edge]struct{}`，
提供 O(1) 的存在性判定。Snapshot 时对键排序，因此无需维护有序结构。

## 候选事务

`Apply` 先对整个批次做纯结构校验（不读状态），再在写锁内把 `nodes`/`edges`
复制为候选副本，按序应用全部操作。任一步失败（ErrExists/ErrNotFound/ErrCycle）
或批次末容量检查（MaxNodes/MaxEdges）失败时直接丢弃候选副本，实现整体回滚；
全部成功才一次性换入并令 generation 加一。空批次不修改状态、不增加 generation。
加边的环检测在候选边上做 BFS（`to` 是否可达 `from`，含自环）。

## 所有权

所有公开方法返回的切片（`Snapshot.Nodes/Edges`、`Coordinator.Decisions`）均为新建
副本，调用方修改不会影响内部状态。`Policy.ReplaceActors` 将白名单构建为全新 map
后在锁内整体替换，读者永远看到一致快照。策略拒绝发生在读取核心状态之前。

## 复杂度

- 结构校验：O(批次数 × 名称长度)。
- 候选复制：O(N + E)；逐操作应用均摊 O(1)，DeleteNode 需扫描边 O(E)。
- 环检测 / Reachable：BFS，O(N + E)。
- 容量检查：O(1)；Snapshot：O(N log N + E log E) 排序。
- 并发：引擎用 `sync.RWMutex`（读路径 Reachable/Snapshot 可并行），Policy 用独立
  `sync.RWMutex`，Coordinator 审计日志用独立 `sync.Mutex` 保证序号连续无间隙。
