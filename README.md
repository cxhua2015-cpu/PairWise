# topologygraph278

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge，加边阻止有向环，删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — 核心事务引擎：`New`/`Apply`/`Reachable`/`Snapshot`。
- `validation.go` — 无副作用批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind 合法、字段约束、名称字符集与字节上限、禁止自环），不读取也不修改图状态。
- `stats.go` — 线性一致统计 `Stats`，在共享读锁下返回同一逻辑时刻的 generation/节点数/边数。
- `clone.go` — 深拷贝 `Clone`，保留逻辑时钟（generation），与并发事务状态一致，所有权完全独立。

## 索引结构

- 节点：`map[string]struct{}`，O(1) 存在性判定。
- 边：`map[Edge]struct{}`（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- 未维护邻接表；可达性通过边集扫描完成，换取删除节点时 O(E) 级联的简洁实现。

## 候选事务（candidate transaction）

`Apply` 先做完整结构预检（`ValidateBatch`），再在写锁内把节点/边复制到候选 map 上顺序执行所有操作：存在性/缺失错误、AddEdge 时在候选图上做 To→From 可达性检查以阻止环。节点/边容量只在全部操作执行完后检查；任何失败直接丢弃候选，原状态零改动（整体回滚）。成功后原子交换候选 map 并使 generation 恰好加一；空批次不增加 generation。

## 所有权与并发

- 所有公开方法并发安全：写操作持 `sync.Mutex` 写锁，`Reachable`/`Snapshot`/`Stats`/`Clone` 持读锁，因此 `Reachable`、`Snapshot`、`Stats` 均观测当前一致快照。
- `Snapshot` 对节点按字典序、边按 (From, To) 稳定排序；返回的切片均为新建，与内部状态隔离。
- `Clone` 复制全部 map 与 generation，克隆体与原图无任何共享内存，双方可独立演进。

## 复杂度

- `ValidateBatch`：O(B·L)，B 为批大小，L 为名称长度。
- `Apply`：O(N + E + B·(E + V)) —— 候选复制 O(N+E)，每个 AddEdge 的环检查为 O(E) 扫描的 DFS。
- `Reachable`：O(E)。`Snapshot`：O(N log N + E log E)。`Stats`：O(1)。`Clone`：O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
