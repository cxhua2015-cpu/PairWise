# topologygraph283

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 多文件架构

- `trustgraph.go` — 核心事务引擎：索引、候选事务、`Apply` / `Reachable` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一套
  `validateBatch` / `validateOp` / `validateName` 语义：未知 kind、额外字段、空名、
  非法字符、超长名称、自环均返回 `ErrInvalidInput`，且不读取任何图状态。
- `stats.go` — `Stats` 在 `RWMutex` 读锁下返回线性一致的 `(Generation, Nodes, Edges)`。
- `clone.go` — `Clone` 深拷贝全部索引与逻辑时钟（generation），与原图完全隔离所有权。

## 索引

- `nodes: map[string]struct{}` — 节点集合，O(1) 存在性判断。
- `edges: map[edgeKey]struct{}` — 边集合，O(1) 查重与删除。
- `out: map[string]map[string]struct{}` — 出边邻接表，支撑可达性 DFS 与节点级联删除。

## 候选事务

`Apply` 先做一次完整结构校验（不读状态），然后在写锁内把当前状态复制为
candidate，在其上顺序应用全部操作；任一步失败（`ErrExists` / `ErrNotFound` /
`ErrCycle`）或最终节点/边数超容量（`ErrCapacity`）即丢弃 candidate，原图不变。
只有全部成功才将 candidate 整体换入并使 generation 恰好 +1；空批次不改变
generation。环检测在 candidate 邻接表上做 DFS：加边 `u→v` 前若 `v` 可达 `u`
则拒绝。

## 所有权与并发

所有公开方法可并发调用：写操作持互斥锁，读操作（`Reachable` / `Snapshot` /
`Stats` / `Clone`）持读锁。`Snapshot` 返回排序后新建切片的深拷贝，`Clone`
逐层复制所有 map，返回值与内部状态无任何共享，调用方可自由修改。

## 复杂度

- 结构校验：O(批次大小 × 名称长度)。
- `Apply`：候选复制 O(N+E)，每操作均摊 O(1)，环检测 O(N+E)，容量检查 O(1)。
- `Reachable`：O(N+E)。`Snapshot`：O(N log N + E log E)。`Stats`：O(1)。`Clone`：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
