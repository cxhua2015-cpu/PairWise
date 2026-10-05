# controlgraph133

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 三层架构

- `trustgraph.go`（状态引擎）：持有事务性数据与快照。
- `policy.go`（策略层）：独立同步、可原子整体替换的 actor 白名单，
  以及单批操作数上限；`Authorize` 绝不触碰核心状态。
- `coordinator.go`（协调层）：先授权再委托状态引擎；成功、拒绝、
  引擎失败都记录到审计日志，序号从 1 连续递增。

## 索引

- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `out map[string]map[string]struct{}`：出边邻接表，供环检测与
  `Reachable` 做 DFS/BFS。

## 候选事务（candidate transaction）

`Apply` 先对整个批次做纯结构校验（未知 kind、多余字段、非法名称
直接返回 `ErrInvalidInput`，不读状态），然后在写锁内把三张 map 克隆
为候选副本，顺序应用全部操作；任一步失败（`ErrExists` / `ErrNotFound` /
`ErrCycle`）或批次末容量超限（`ErrCapacity`）都直接丢弃副本，原状态
不变。全部成功才一次性替换内部状态并将 `generation` 加一；空批次不
改变 generation。

## 所有权

- `Snapshot` 返回新建并排序的切片（节点字典序，边按 `(From, To)`），
  调用方修改不影响内部状态。
- `Policy.ReplaceActors` 复制输入切片，白名单整体原子替换。
- `Coordinator.Decisions` 返回审计日志副本，不别名内部存储。

## 并发与复杂度

- 状态引擎用 `sync.RWMutex`：`Apply` 独占，`Reachable`/`Snapshot` 共享读。
- 策略层用独立的 `sync.RWMutex`；协调层用独立 `sync.Mutex` 保护序号
  与日志，三层可并发安全调用。
- 设批次操作数 k、节点数 n、边数 m：结构校验 O(k·L)（L 为名称长度）；
  候选克隆 O(n+m)；每次 `AddEdge` 环检测最坏 O(n+m)；容量检查 O(1)；
  `Snapshot` O(n log n + m log m)；`Reachable` O(n+m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
