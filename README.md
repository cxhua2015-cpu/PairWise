# topologygraph398

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 去重与删除。
- 无独立邻接表；可达性通过边集扫描 DFS 计算，换取极简内存与实现。

## 候选事务（原子批次）

`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集/长度、节点操作不得带 `To`），
不读取任何状态；随后在写锁内把节点/边索引克隆为候选副本，按序在副本上执行操作
（存在性、缺失、成环检查），最后在批次末统一检查节点/边容量。任一步失败直接丢弃
候选副本——原图零改动，实现整体回滚；成功则一次性替换索引并将 `generation` 加一。
空批次与失败批次不改变 `generation`。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持 `RWMutex` 读锁，
  因此 `Reachable` 与 `Snapshot` 总是观察到同一代的一致状态。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），
  调用方对返回切片的修改不会影响内部状态，反之亦然。

## 复杂度

设 V 为节点数、E 为边数、B 为批次操作数：

- `Apply`：结构校验 O(B·名称长度)；候选克隆 O(V+E)；DeleteNode 级联删边 O(E)；
  AddEdge 成环检查为 DFS，O(V+E)；整体 O(B·(V+E))，空间 O(V+E)。
- `Reachable`：O(V+E) DFS。
- `Snapshot`：O(V log V + E log E) 排序。
