# servicegraph

并发安全的内存型服务依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 的 DFS 使用；与 `edges` 始终同步维护。

## 候选事务

`Apply` 先在持锁状态下把 `nodes`/`edges`/`out` 拷贝为候选副本，所有操作只作用于副本；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接丢弃副本，实现整体回滚。全部成功才一次性替换内部状态并将 `generation` 加一（空批次不增加）。结构校验（kind、名称字符集与字节上限、多余字段）在读取任何状态之前完成，失败返回 `ErrInvalidInput`。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，读写互斥。
- `Snapshot` 返回的切片是新分配并按节点名、边 `(From, To)` 稳定排序的副本，调用方修改不影响内部状态。
- `Reachable` 在读锁内基于当前一致快照做 DFS，不会观察到批次中间态。

## 复杂度

设 N 为节点数、E 为边数、K 为批次内操作数：

- `Apply`：拷贝 O(N+E)，每操作均摊 O(1)，加边环检测 O(N+E)，合计 O(N+E+K·(N+E))（最坏）。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N+E) 构造 + O(N log N + E log E) 排序。
- 空间：O(N+E)。
