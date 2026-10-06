# topologygraph263

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 多文件架构

- `trustgraph.go` — 核心事务引擎：索引结构、`Apply`、`Reachable`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套
  `validateOp` 结构语义（已知 kind、名称字符集/字节上限、无多余字段、禁止自环），
  不读取也不修改任何图状态。
- `stats.go` — 线性一致统计：`Stats` 在单次读锁内同时读取 generation、节点数、
  边数，三者永远对应同一个已提交状态。
- `clone.go` — 深拷贝：`Clone` 保留逻辑时钟（generation），并逐层复制所有
  map，克隆体与原图完全隔离所有权，互不影响。

## 索引

- `nodes map[string]struct{}`：节点成员判定 O(1)。
- `edges map[Edge]struct{}`：边成员判定 O(1)。
- `adj map[string]map[string]struct{}`：出边邻接表，支撑可达性 DFS 与
  删除节点时的级联清理。

## 候选事务（candidate transaction）

`Apply` 先做完整结构校验（不读状态），再在写锁内把 `nodes`/`edges`/`adj`
复制为候选副本，按序应用全部操作；任一步失败或批次末容量超限（`ErrCapacity`）
直接丢弃候选，已提交状态零改动，实现整体回滚。非空成功批次 generation 恰好
加一，空批次不变。

## 所有权

所有公开方法返回的切片（`Snapshot`、`Stats` 值、`Clone` 的内部 map）均为新建
副本，调用方对返回值的任何修改都不会影响图内部状态；`Clone` 之后两个图的
后续变更互不别名。

## 并发与复杂度

- 单一 `sync.RWMutex`：写事务持写锁，`Reachable`/`Snapshot`/`Stats`/`Clone`/
  `ValidateBatch` 可并发读。
- `Apply`：结构校验 O(L)，候选复制 O(V+E)，每个 AddEdge 的成环检查为一次
  DFS O(V+E)，整批 O(V+E+L·(V+E))。
- `Reachable`：O(V+E)；`Snapshot`：O(V log V + E log E) 稳定排序；
  `Stats`：O(1)；`Clone`：O(V+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
