# topologygraph288

并发安全的内存型“控制拓扑图 288”，Go 1.22+，仅依赖标准库。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联
删除关联边，容量只在批次末检查，失败整体回滚。

## 多文件架构

- `trustgraph.go` — 核心事务引擎：`Apply`、`Reachable`、`Snapshot`、候选事务与环检测。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义
  （`validateStructural`），不读取、不修改图状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回同一逻辑时钟点的 generation/节点数/边数。
- `clone.go` — 深拷贝：`Clone` 保留逻辑时钟（generation），所有权完全隔离。

## 索引

图状态由三张表构成，均由同一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}` — 节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}` — 边集合（`Edge{From,To}` 为可比较键），O(1) 去重。
- `adj map[string]map[string]struct{}` — 出边邻接表，供环检测与 `Reachable` 遍历，
  同时支撑 `DeleteNode` 的级联删除（出边直接命中，入边扫描邻接表）。

## 候选事务

`Apply` 先在持锁状态下把已提交状态深拷贝为候选事务（`candidateLocked`），逐条在
候选上执行操作并做状态检查（`ErrExists` / `ErrNotFound` / `ErrCycle`），最后才检查
节点/边容量（`ErrCapacity`）。任一步失败直接丢弃候选，已提交状态零改动；成功则
整体换入并令 generation 恰好 +1。空批次不改变 generation。结构校验（未知 kind、
非法名称、自环、多余字段 → `ErrInvalidInput`）在读取任何状态之前完成。

## 所有权

- `Snapshot` 返回新建切片（节点、边分别按字典序与 `(From,To)` 稳定排序），调用方
  修改返回值不影响内部状态。
- `Clone` 在写锁保护下复用候选拷贝逻辑，返回的图与原图不共享任何 map，逻辑时钟
  一并复制；两侧后续互不可见。
- 候选事务只在单个 `Apply` 调用内使用，从不对外暴露。

## 复杂度

设 N 为节点数、E 为边数、K 为批次操作数：

- `Apply`：O(N + E) 拷贝候选 + 每操作 O(1) 摊销；每次 `AddEdge` 的环检测为
  O(N + E) DFS，故整体 O(K·(N + E))。
- `Reachable`：O(N + E) BFS/DFS，读锁下基于当前一致快照。
- `Snapshot`：O(N log N + E log E) 排序。
- `Stats`：O(1)。`Clone`：O(N + E)。
- `ValidateBatch`：O(K·L)，L 为名称字节上限，不触碰图状态。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
