# topologygraph288

并发安全的内存型有向无环控制拓扑图（Go 1.22+，仅标准库）。

## 设计

### 索引
`Graph` 内部状态 `state` 维护四份冗余索引：`nodes`（节点集合）、`edges`（边集合）、
`out` / `in`（出/入邻接表）。边集合保证 O(1) 判重，邻接表让 `Reachable` 的 BFS、
加边时的环检测以及 `DeleteNode` 的级联删边都只访问相关边，而非全图扫描。

### 候选事务
`Apply` 先在 `validation.go` 中做**无副作用的完整结构预检**（`ValidateBatch`，
不读状态），再在写锁内把当前状态深拷贝为候选状态，顺序应用所有操作。
任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃候选；全部成功后
才在批次末检查节点/边容量（`ErrCapacity`），通过则原子换入候选并将
`generation` 加一。空批次成功但不推进逻辑时钟。因此批次是原子的：
失败整体回滚，对外无任何部分可见的中间状态。

### 所有权
所有返回的切片（`Snapshot` 的 `Nodes`/`Edges`）都是新分配的拷贝，与内部状态
完全隔离；`Clone` 在读锁下深拷贝全部索引并保留逻辑时钟（`generation`），
克隆体与原图互不影响。`Stats` 在读锁下汇总，是线性一致的。

### 并发
单把 `sync.RWMutex`：写路径（`Apply`）独占，读路径
（`Reachable`/`Snapshot`/`Stats`/`Clone`）共享。`ValidateBatch` 只读配置，
无需加锁。所有公开方法可并发调用，`go test -race` 通过。

### 复杂度
- `Apply`：结构预检 O(L)，L 为批次总字节；候选拷贝 O(N+E)；每个 `AddEdge`
  的环检测为一次 BFS，O(N+E)；容量检查 O(1)。
- `Reachable`：BFS，O(N+E)。
- `Snapshot`：O(N log N + E log E)，节点按字典序、边按 `(From, To)` 稳定排序。
- `Stats`：O(1)；`Clone`：O(N+E)。

## 文件
- `trustgraph.go`：类型、错误值、状态索引、原子事务 `Apply`、`Reachable`、`Snapshot`
- `validation.go`：名称/操作结构校验与无副作用预检 `ValidateBatch`
- `stats.go`：线性一致统计 `Stats`
- `clone.go`：保留逻辑时钟的深拷贝 `Clone`

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
