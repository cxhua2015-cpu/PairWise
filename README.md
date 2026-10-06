# topologygraph273

并发安全的内存型“控制拓扑图 273”，Go 1.22+，仅依赖标准库。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联
删除关联边，容量仅在批次末检查，失败整体回滚。

## 架构

- `trustgraph.go` — 核心事务引擎：`New` / `Apply` / `Reachable` / `Snapshot`。
- `validation.go` — 无副作用批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义
  （未知 kind、额外字段、非法名称、自环均返回 `ErrInvalidInput`）。
- `stats.go` — 线性一致的状态统计 `Stats`（读锁下读取，与并发事务一致）。
- `clone.go` — 保留逻辑时钟（generation）且所有权完全隔离的深拷贝 `Clone`。

## 索引

- `nodes map[string]struct{}`：O(1) 节点存在性查询。
- `edges map[Edge]struct{}`：O(1) 边存在性查询，`Edge{From, To}` 为可比较键。
- 未维护邻接表；可达性与环检测按需遍历边集（见复杂度）。

## 候选事务（candidate transaction）

`Apply` 先在 `ValidateBatch` 下做完整结构校验（不读状态），再于写锁内把当前
`nodes`/`edges` 复制为候选状态，按序在候选上应用全部操作（存在性、环检测均针对
候选状态），最后在批次末检查最终节点/边容量。任一步失败直接丢弃候选，原状态零
改动——天然回滚。仅当批次非空且成功时 `generation` 递增一次；空批次不变。

## 所有权

- 所有公开方法并发安全：写路径持 `sync.Mutex` 写锁，`Reachable` / `Snapshot` /
  `Stats` / `Clone` 持读锁。
- `Snapshot` 返回新建切片（节点、边均稳定排序），`Clone` 逐键复制 map，返回值与
  内部状态完全隔离，调用方修改不影响图。
- `Clone` 连同 `generation` 一起复制，克隆体后续演化与原体互不影响。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数：

- `ValidateBatch`：O(B · L)，L 为名称长度上限。
- `Apply`：O(N + E + B · E)，环检测为每次 `AddEdge` 一次 O(E) 可达性遍历；
  `DeleteNode` 级联删除为 O(E)。
- `Reachable`：O(E)。`Snapshot`：O(N log N + E log E)（排序）。
- `Stats`：O(1)。`Clone`：O(N + E)。空间：O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
