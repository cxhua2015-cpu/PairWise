# topologygraph423

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 架构与索引

- `trustgraph.go`：核心事务引擎。状态由两个哈希索引承载：
  `nodes map[string]struct{}`（O(1) 节点存在性）与
  `edges map[Edge]struct{}`（O(1) 边存在性/去重）。
  单个 `sync.RWMutex` 保护全部状态与逻辑时钟 `generation`，
  写操作独占、读操作（`Reachable`/`Snapshot`/`Stats`/`Clone`）共享。
- `validation.go`：无副作用的结构预检（kind 合法、字段位置、
  名称字符集与字节上限、禁止自环），`Apply` 与 `ValidateBatch` 共享同一实现。
- `stats.go`：在同一把读锁下读取的线性一致统计。
- `clone.go`：深拷贝全部索引与逻辑时钟，所有权完全隔离。
- `preview.go`：事务预演。

## 候选事务与所有权

`Apply` 先在候选副本（节点/边索引的拷贝）上顺序执行全部操作，
任一步失败或批次末容量超限即丢弃候选，原状态零改动；成功才一次性换入
并对非空批次将 `generation` 加一。`Preview` 在同一读锁临界区内
`Clone` 出候选图，再复用完整 `Apply` 语义提交候选，返回候选的
`Result`/`Snapshot`/`Stats`，原对象的状态、时钟与所有权不变；
失败时错误与 `Apply` 一致且其余返回值全为零。所有返回切片均为新建拷贝，
与内部状态及候选状态隔离。

## 复杂度

- 结构校验：O(批次数 × 名称长度)。
- `Apply`：候选拷贝 O(N+E)；每个 `AddEdge` 的环检测为一次 DFS，O(N+E)；
  `DeleteNode` 级联扫描 O(E)。
- `Reachable`：O(N+E)；`Snapshot`：O(N log N + E log E) 稳定排序；
  `Stats`：O(1)；`Clone`/`Preview`：O(N+E) 外加一次候选事务开销。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
