# topologygraph263

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持

`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，
容量仅在批次末检查，失败整体回滚。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `topologygraph263/trustgraph.go` — 核心类型、索引结构、事务引擎（`Apply`）、`Reachable`、`Snapshot`。
- `topologygraph263/validation.go` — 无副作用批次预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义。
- `topologygraph263/stats.go` — 线性一致的 `Stats` 统计。
- `topologygraph263/clone.go` — 保留逻辑时钟（generation）且所有权完全隔离的深拷贝。

## 索引结构

`Graph` 持有四份冗余索引，全部在单次 `sync.RWMutex` 写临界区内一致更新：

- `nodes map[string]struct{}` — 节点存在性，O(1) 查询。
- `edges map[Edge]struct{}` — 边存在性，O(1) 去重。
- `out map[string]map[string]struct{}` — 出邻接表，用于环检测与 `Reachable` 的 BFS。
- `in map[string]map[string]struct{}` — 入邻接表，使 `DeleteNode` 级联删除入边为 O(度数)。

## 候选事务（candidate transaction）

`Apply` 先在持锁状态下把已提交状态复制为私有候选副本，再在候选上顺序执行全部操作；
任何操作失败或批次末容量（`MaxNodes`/`MaxEdges`）超限即直接丢弃候选，已提交状态零改动，
实现整体回滚。仅当整个批次成功时才用候选替换提交状态并将 `generation` 加一；
空批次不推进 generation。结构校验（`ValidateBatch`）在任何状态读取之前完成。

## 所有权与隔离

- 所有公开方法并发安全：写操作独占锁，读操作（`Reachable`/`Snapshot`/`Stats`/`Clone`）走读锁，
  因此 `Reachable` 与 `Stats` 观测的是单一线性化点的一致快照。
- `Snapshot` 返回的节点与边切片为新分配内存并按字典序稳定排序，调用方修改不影响内部状态。
- `Clone` 深拷贝全部 map 与邻接表并保留 `generation`，克隆体与原图不共享任何可变状态。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数：

- `ValidateBatch`：O(B · 名称长度)，纯结构校验，不读状态。
- `Apply`：O(N + E) 候选复制 + 每操作 O(1) 均摊；`AddEdge` 环检测为一次 BFS，O(N + E)；
  `DeleteNode` 级联为 O(该节点度数)。
- `Reachable`：一次 BFS，O(N + E)。
- `Snapshot`：O(N log N + E log E) 排序。
- `Stats`：O(1)。`Clone`：O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
