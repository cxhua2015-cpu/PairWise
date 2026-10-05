# controlgraph173

并发安全的内存型“控制面依赖图 173”。原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。仅依赖标准库（Go 1.22+）。语义细节见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：边集合，键为 `{From, To}`。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(度数) 级联清理关联边。

## 候选事务

`Apply` 先做纯结构校验（不读状态），再在写锁内把 `nodes/edges/out/in` 克隆为候选副本，按序应用全部操作；任一步失败或批次末容量超限即丢弃候选，原状态不变（整体回滚）。全部成功才原子替换内部状态并将 `generation` 加一；空批次不增加 generation。

## 所有权

- 所有公开方法通过 `sync.RWMutex` 并发安全：写路径（`Apply`）持写锁，读路径（`Reachable`、`Snapshot`）持读锁。
- `Snapshot` 返回新建并排序的切片，`Reachable` 只返回标量；返回值与内部状态完全隔离，调用方可自由修改。
- 图不回读、不保留调用方传入的 `Batch`/`Op` 数据。

## 复杂度

设 V 为节点数、E 为边数、k 为批次操作数：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选克隆 O(V+E)；环检测每次加边一次 DFS，O(V+E)；整体 O(V + E + k·(V+E))。
- `Reachable`：一次 DFS，O(V+E)。
- `Snapshot`：收集 O(V+E)，排序 O(V log V + E log E)。
- `New`：O(1)。空间 O(V+E)。
