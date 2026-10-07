# topologygraph338

并发安全的内存型控制拓扑图（见 `SPEC.md`）。原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge，加边阻止有向环，删节点级联删边，容量只在批次末检查并整体回滚。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From,To}` 为可比较键），O(1) 查重与删除。
- 邻接表不持久化；可达性检查时按需从边集构建，避免双索引不一致。

## 候选事务

`Apply` 先做纯结构校验（kind、名称字符集与字节上限、多余字段），不读状态；随后在深拷贝的节点/边副本上顺序应用全部操作，任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本。全部成功后在副本上检查最终容量，超限返回 `ErrCapacity`；只有全部通过才把副本一次性交换进图并（非空批次）将 generation 加一，因此失败批次对状态完全无影响。

## 所有权与并发

- 单个 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，读写各自看到一致快照。
- `Snapshot` 返回新建的切片（节点字典序、边按 `(From,To)` 字典序稳定排序），调用方修改返回值不影响内部状态。

## 复杂度

设批次 op 数 k、节点数 n、边数 m：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；克隆 O(n+m)；每条 AddEdge 的环检测为一次 BFS，O(n+m)；容量检查 O(1)。整体 O(k·(n+m))。
- `Reachable`：一次 BFS，O(n+m)。
- `Snapshot`：O(n log n + m log m) 排序，空间 O(n+m)。
