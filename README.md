# controlgraph168

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：边集合（`Edge{From, To}` 为可比较键）。
- `out map[string]map[string]struct{}`：出边邻接表，与 `edges` 同步维护，用于环检测与 `Reachable` 的遍历。

## 候选事务（candidate transaction）

`Apply` 分两阶段：先对整个批次做完整结构校验（kind 合法、字段不多余、名称合法），不读取任何状态；然后在写锁内把 `nodes/edges/out` 复制为候选状态，按序在候选状态上执行全部操作（存在性、环检测逐条进行）。任一操作失败直接返回，候选状态被丢弃，实现整体回滚。节点/边容量只在批次末对候选状态检查，超限返回 `ErrCapacity` 并回滚。仅当非空批次全部成功时，候选状态一次性替换内部状态，`generation` 恰好加一；空批次不改变 generation。

## 所有权

所有公开方法返回的切片与字符串均为新建副本，`Snapshot` 的 `Nodes`/`Edges` 与内部 map 不共享内存，调用方修改返回值不影响图状态。`Apply` 不保留传入 `Batch` 的引用。

## 并发与复杂度

- 并发：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁（`sync.RWMutex`），所有公开方法可并发调用。
- 结构校验：O(L)，L 为批次操作数。
- 候选复制：O(V + E)。
- `AddEdge` 环检测与 `Reachable`：DFS，O(V + E)。
- `DeleteNode`：O(E) 扫描删除关联边。
- `Snapshot`：O(V log V + E log E) 稳定排序（节点按字典序，边按 `(From, To)`）。
