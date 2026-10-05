# subscriptiongraph

并发安全的内存型订阅依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

图状态由四份互为冗余的索引组成，全部随每次成功提交原子替换：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边存在性，O(1) 判重。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与删除节点时清理出边。
- `in map[string]map[string]struct{}`：入边邻接表，用于删除节点时清理入边。

## 候选事务（candidate transaction）

`Apply` 先在持锁状态下克隆当前状态得到候选副本，按批次顺序在副本上逐条执行
AddNode/DeleteNode/AddEdge/DeleteEdge。任何一步失败（ErrExists/ErrNotFound/ErrCycle）
直接丢弃副本；全部执行完后才检查最终节点/边容量（ErrCapacity），通过则整体替换
内部状态并将 generation 加一。因此批次是原子的：失败批次对图完全无副作用，
空批次不改变 generation。结构校验（未知 kind、多余字段、非法名称）在读状态之前
对整个批次完成，返回 ErrInvalidInput。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，
  因此 Reachable 总是基于某一已提交 generation 的一致快照。
- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片（节点按字典序、边按
  (From, To) 稳定排序），调用方修改返回值不影响图内部状态。
- 候选副本在提交前归当前 `Apply` 调用独占，提交后归图所有，永不就地修改。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数：

- `New`：O(1)。
- `Apply`：克隆 O(N+E)，每条操作 O(1) 均摊；AddEdge 的环检测为一次 DFS，
  O(N+E)；总复杂度 O(N+E+B·(N+E))，空间 O(N+E)。
- `Reachable`：O(N+E)，O(N) 额外空间。
- `Snapshot`：O(N+E) 构造，排序 O(N log N + E log E)。
