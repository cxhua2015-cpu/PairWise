# topologygraph318

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的图遍历；删除节点时通过扫描 `edges` 级联清理关联边并同步维护 `out`。

## 候选事务（原子批次）

`Apply` 先在无锁状态下做完整结构校验（kind、名称字符集/长度、多余字段），再持写锁把当前状态**深拷贝**为候选图，在候选图上顺序执行全部操作；任一操作失败或批次末节点/边数超过 `Options` 容量上限时直接丢弃候选图，原状态不变（整体回滚），generation 不变。全部成功才把候选图的三个 map 与 `generation+1` 一次性提交。空批次直接返回当前 generation，不自增。

## 所有权与并发

- 单个 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发执行。
- `Snapshot` 返回的切片与 map 均为新建拷贝，调用方修改不影响内部状态；返回后图继续演化也不影响已返回的快照。
- `Reachable` 在读锁内遍历，看到的始终是某一批次提交后的一致快照。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数：

- `Apply`：结构校验 O(B·L)（L 为名称长度上限）；候选克隆 O(N+E)；每个 AddEdge 的环检测为一次 DFS，O(N+E)；DeleteNode 级联 O(E)；整体 O(N+E+B·(N+E))。
- `Reachable`：一次 BFS/DFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)。
- 空间：O(N+E)。
