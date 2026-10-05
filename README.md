# trustgraph

并发安全的内存型信任依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 实现说明

**索引**
- `nodes`：`map[string]struct{}` 节点集合。
- `out` / `in`：正向与反向邻接索引（`map[string]map[string]struct{}`），加删边与删节点级联均为 O(度数)，`Reachable` 沿 `out` 做 DFS。
- 边数以计数器 `edges` 维护，容量检查 O(1)。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind 合法、节点操作无多余字段、名称字符集与长度），再读取任何状态。
- 之后在写锁内把 `nodes`/`out`/`in` 复制为候选副本，按顺序应用全部操作；任何 `ErrExists`/`ErrNotFound`/`ErrCycle` 直接丢弃副本，原状态不变。
- 节点/边容量只在批次末对最终候选状态检查，超限返回 `ErrCapacity` 并整体回滚；因此“删一个再加一个”在满容量下也能成功。
- 仅当非空批次成功时提交副本并将 `generation` 加一；空批次与失败批次不改变 generation。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，彼此可并行。
- `Reachable` 在读锁内基于当前一致快照计算。
- `Snapshot` 对节点按字典序、边按 `(From, To)` 稳定排序，并返回全新分配的切片；调用方修改返回值不影响内部状态。
- 加边前在候选图上检查 `To` 是否可达 `From`（含自环），可达则返回 `ErrCycle`。

**复杂度**（V 节点数，E 边数，B 批次操作数）
- `Apply`：结构校验 O(B)；候选复制 O(V+E)；逐操作 O(1) 均摊，`AddEdge` 的环检测 O(V+E)；末次容量检查 O(1)。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V+E) 构造，排序 O(V log V + E log E)。
- 空间：O(V+E)。
