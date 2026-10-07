# topologygraph433

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点
级联删除关联边，容量只在批次末检查，失败整体回滚。

## 架构

- `trustgraph.go` — 核心事务引擎：`Graph`、索引结构、`Apply` / `Reachable` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一份
  `validateBatch` / `validName` 语义，保证两者判定一致。
- `stats.go` — `Stats`：在读锁内一次性读取 generation、节点数、边数，线性一致。
- `clone.go` — `Clone`：深拷贝全部索引并保留逻辑时钟（generation），与原型完全隔离。
- `preview.go` — `Preview`：在一次线性化快照上复用完整事务语义（结构校验 → 顺序应用 →
  批次末容量检查），返回候选 `Result` / `Snapshot` / `Stats`；原对象状态、generation
  与所有权不变，错误及优先级与同状态 `Apply` 完全一致，失败时返回值全部为零值。

## 索引

`Graph` 维护三类索引：`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）、
`adj`（出邻接表 `from -> {to}`）。三者同事务更新，互为冗余以换取 O(1) 的
存在性判定与高效的环检测 / 可达性遍历。

## 候选事务

`Apply` 与 `Preview` 都不就地修改：先在当前一致快照上构造候选状态
（`lockedCloneState`），按序应用全部操作，最后在批次末检查 `MaxNodes` / `MaxEdges`
容量；任何一步失败即丢弃候选，原状态与 generation 不变（回滚）。非空成功批次
generation 恰好加一，空批次不变。

## 所有权

所有返回的切片（`Snapshot.Nodes` / `Snapshot.Edges`）都是新建并排序后的副本，
与内部状态及候选状态完全隔离；`Clone` 深拷贝全部 map，克隆体与原型互不影响；
`Preview` 的候选结果同样不共享内部内存。

## 复杂度

- `AddNode` / `DeleteEdge` / 存在性检查：O(1)。
- `AddEdge`：O(V + E)（DFS 环检测）。
- `DeleteNode`：O(V + E)（级联删除出入边）。
- `Reachable`：O(V + E)。
- `Snapshot`：O(V log V + E log E)（稳定排序）。
- `Apply` / `Preview`：O(批次大小 × 单操作代价 + V + E)（候选拷贝）。
- 空间：O(V + E)。

## 并发

全部公开方法由一把 `sync.RWMutex` 保护：写路径（`Apply` / `Preview`）取写锁，
读路径（`Reachable` / `Snapshot` / `Stats` / `Clone` / `ValidateBatch`）取读锁，
保证线性一致与并发安全。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
