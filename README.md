# topologygraph223

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查、失败整体回滚。

## 架构与索引

实现分布在四个联动文件中：

- `trustgraph.go`：核心事务引擎。状态由四份索引组成：
  - `nodes: map[string]struct{}` —— 节点集合，O(1) 存在性判断；
  - `edges: map[Edge]struct{}` —— 边集合，O(1) 去重与删除；
  - `out: map[string]map[string]struct{}` —— 出边邻接表，用于
    `Reachable` 与加边前的环检测（DFS/BFS）；
  - `in: map[string]map[string]struct{}` —— 入边邻接表，使
    `DeleteNode` 级联删除只触及关联边而非全图扫描。
- `validation.go`：无副作用批次预检。`ValidateBatch` 只读取不可变的
  `Options` 上限，校验 kind/字段/名称字符集（非空 ASCII `[a-z0-9-_]`，
  受 `MaxNameBytes` 约束）、未知 kind 与多余字段返回 `ErrInvalidInput`。
  `Apply` 复用同一套结构语义，先完整预检再读取状态。
- `stats.go`：`Stats` 在 `RWMutex` 读锁下返回 `{generation, nodes, edges}`，
  相对并发事务是线性一致的。
- `clone.go`：`Clone` 在读锁下复用候选拷贝逻辑做深拷贝，保留逻辑时钟
  （generation），与源图零共享可变不可变配置外的任何可变状态。

## 候选事务（candidate transaction）

`Apply` 在写锁内把四份索引浅拷贝为候选视图（copy-on-write），按顺序在
候选上应用全部操作：存在性/缺失（`ErrExists`/`ErrNotFound`）、环
（`ErrCycle`）即时判定；节点/边容量只在批次末对最终计数检查
（`ErrCapacity`）。任一步失败直接丢弃候选即整体回滚，已提交状态与
generation 不变；全部成功才一次性换入候选索引并将 generation 加一
（空批次不加）。因此并发读者永远看到完整一致的已提交状态。

## 所有权

所有公开方法返回的切片（`Snapshot.Nodes`、`Snapshot.Edges`）与克隆体
均为新建内存，调用方可自由修改而不影响图内部状态；`Clone` 后的图与
源图互不影响。所有公开方法可并发调用，内部以 `sync.RWMutex` 保护：
写事务互斥，读（`Reachable`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）
可并发。

## 复杂度

设批次大小为 B，节点数 N，边数 E：

- `Apply`：候选拷贝 O(N+E)，每个操作 O(1) 均摊；`AddEdge` 环检测
  O(N+E) 最坏；容量检查 O(1)。
- `Reachable`：O(N+E)（DFS，读锁内对当前一致快照执行）。
- `Snapshot`：O(N+E) 构造 + O(N log N + E log E) 稳定排序（节点按字典
  序，边按 (From, To) 字典序）。
- `Stats`：O(1)；`Clone`：O(N+E)；`ValidateBatch`：O(B·L)，L 为名称
  字节上限，不触碰图状态。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
