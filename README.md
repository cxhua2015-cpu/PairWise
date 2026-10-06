# topologygraph243

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 多文件架构

- `trustgraph.go` — 核心事务引擎：状态、索引、候选事务提交、`Apply` / `Reachable` / `Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（`validateBatch`），只校验 kind、名称字符集/长度与多余字段，绝不读取或修改状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁内一次性读取 generation、节点数、边数。
- `clone.go` — 深拷贝：`Clone` 复制全部索引与逻辑时钟（generation），与源图完全隔离所有权。

## 索引

- `nodes map[string]struct{}` — 节点存在性 O(1)。
- `edges map[Edge]struct{}` — 边存在性 O(1)。
- `adj map[string]map[string]struct{}` — 出边邻接表，供环检测与 `Reachable` 的 DFS 使用。

## 候选事务（candidate transaction）

`Apply` 先经共享的 `validateBatch` 做完整结构预检（此时不读状态），然后在写锁内
把 `nodes`/`edges`/`adj` 复制为候选副本，顺序应用全部操作；任何一步失败
（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败（`ErrCapacity`）
都直接丢弃候选副本，实现整体回滚。全部成功才一次性交换进图，非空批次
generation 恰好 +1，空批次不变。

## 所有权

- 所有公开方法在 `sync.RWMutex` 下执行，可并发调用；`Apply` 持写锁，
  `Reachable`/`Snapshot`/`Stats`/`Clone` 持读锁，读到的是同一线性化点的一致快照。
- `Snapshot` 返回排序后的新切片，`Clone` 返回全新 map 与切片，调用方修改返回值
  不影响内部状态；`Clone` 之后的图与源图无任何共享内存。

## 复杂度

- 结构预检：O(批次大小)。
- `Apply`：候选复制 O(N+E)，每操作 O(1)（加边另加一次 DFS 环检测 O(N+E)），
  批次末容量检查 O(1)。
- `Reachable`：DFS，O(N+E)。
- `Snapshot`：O(N log N + E log E)（稳定排序）。
- `Stats`：O(1)。`Clone`：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
