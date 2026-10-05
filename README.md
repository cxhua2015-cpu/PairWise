# controlgraph188

并发安全的内存型“控制面依赖图”。原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge，加边阻止有向环，删除节点级联删除关联边，容量只在批次末检查、失败整体回滚。仅依赖标准库，需 Go 1.22+。

## 设计说明

- **索引**：图状态由四份索引组成——`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）、`out`（邻接表 From→To 集合）、`in`（逆邻接表）。`out`/`in` 使可达性遍历、删点级联和环检测均为邻接复杂度而非全边扫描。
- **候选事务**：`Apply` 先做整批结构校验（kind、多余字段、名称字符与字节上限），再在持写锁期间把状态浅拷贝为候选事务（candidate），逐操作作用于候选；任一步失败（ErrExists/ErrNotFound/ErrCycle）或批次末容量超限（ErrCapacity）直接丢弃候选，已提交状态不变，实现原子回滚。成功后整体换入候选，非空批次 generation 恰好 +1，空批次不变。
- **所有权与并发**：所有公开方法经 `sync.RWMutex` 保护（Apply 写锁，Reachable/Snapshot 读锁）。`Snapshot` 返回新分配的、按节点名与 `(From,To)` 稳定排序的切片，与内部状态完全隔离；`Reachable` 在读锁内基于当前一致快照做 DFS。调用方对返回值拥有独占所有权。
- **复杂度**：设批次长度 B、节点数 N、边数 E。结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；AddNode/DeleteEdge O(1) 均摊，DeleteNode O(度数)，AddEdge 的环检测 O(N+E) 最坏；容量检查 O(1)；Snapshot O(N log N + E log E)；Reachable O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
