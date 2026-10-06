# topologygraph248

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与状态视图：

- `trustgraph.go` — 核心事务引擎：`New` / `Apply` / `Reachable` / `Snapshot`，以及候选事务状态机。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 在读取任何状态之前调用同一函数，保证“先完整结构校验，再读取状态”。
- `stats.go` — `Stats` 在 `RLock` 下读取，返回与某一事务点线性一致的 `{Generation, Nodes, Edges}`。
- `clone.go` — `Clone` 在 `RLock` 下复制全部索引与逻辑时钟（generation），返回所有权完全独立的深拷贝。

## 索引

图维护四份互相一致的内存索引，全部随事务原子提交：

- `nodes: map[string]struct{}` — 节点集合。
- `edges: map[Edge]struct{}` — 边集合（`Edge{From, To}` 为可比较键）。
- `out: map[string]map[string]struct{}` — 出邻接表，用于可达性 BFS 与环检测。
- `in: map[string]map[string]struct{}` — 入邻接表，用于 `DeleteNode` 时 O(关联度) 级联删边。

## 候选事务（candidate transaction）

`Apply` 持写锁后先把四份索引深拷贝为候选状态 `state`，所有操作按序作用于候选；任一步失败（`ErrExists` / `ErrNotFound` / `ErrCycle`）或批次末容量检查失败（`ErrCapacity`）时直接丢弃候选，已提交状态零改动，实现整体回滚。成功时 `commit` 将候选 map 指针整体换入并把 generation 恰好加一；空批次不增加 generation。容量（`MaxNodes`/`MaxEdges`）只在批次末对最终计数检查，因此批次内允许临时超限（如先删后增）。

## 所有权

- 候选状态与 `Clone` 的结果都是全新分配的 map，提交/返回后与源图无任何共享内存。
- `Snapshot` 的 `Nodes`/`Edges` 为新切片，排序（节点字典序、边按 `(From, To)` 字典序）后返回，调用方修改不影响内部状态。
- 所有公开方法通过一把 `sync.RWMutex` 保护：写路径（`Apply`）独占，读路径（`Reachable`/`Snapshot`/`Stats`/`Clone`）共享，均可并发调用。

## 复杂度

设批次大小为 B，节点数 N，边数 E，节点关联度为 d：

- `Apply`：候选拷贝 O(N + E)，之后每操作均摊 O(1)；`AddEdge` 的环检测为一次 BFS，O(N + E)；整体 O(N + E + B·(N + E)) 上界。
- `DeleteNode`：O(d)，级联删除关联边。
- `Reachable`：O(N + E)（BFS，读锁下使用当前一致快照）。
- `Snapshot`：O(N log N + E log E)（稳定排序）。
- `Stats`：O(1)；`Clone`：O(N + E)；`ValidateBatch`：O(B)，不读写图状态。
