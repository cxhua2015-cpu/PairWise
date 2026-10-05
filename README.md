# controlgraph143 — 控制面依赖图

并发安全的内存型有向依赖图，Go 1.22+，仅标准库。三层架构：

- `trustgraph.go` — 状态引擎：原子批次、快照、可达性。
- `policy.go` — 策略层：可原子替换的 actor 白名单 + 单批操作数上限。
- `coordinator.go` — 协调层：先授权再调用引擎，并为成功/拒绝/引擎失败分配连续审计序号。

## 索引

引擎维护三类内存索引：`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）、`out`（出边邻接表 `map[from]map[to]`）。所有查询与环检测只走 `out`，避免全边扫描；`DeleteNode` 通过遍历边集清理关联边并同步邻接表。

## 候选事务（candidate transaction）

`Apply` 先做**完整结构校验**（kind 合法、节点操作不带 `To`、名称非空且仅含 `[a-z0-9-_]` 且不超 `MaxNameBytes`），不通过返回 `ErrInvalidInput` 且不读状态。随后在写锁内把 `nodes/edges/out` 深拷贝为候选状态，按序应用全部操作（存在性 → `ErrExists`/`ErrNotFound`，加边前用 DFS 检查 `To` 是否可达 `From` 以阻止环 → `ErrCycle`）。**容量只在批次末检查**（节点/边数超上限 → `ErrCapacity`）。任一步失败直接丢弃候选状态，即整体回滚；全部成功才一次性换入并使 `generation` 恰好 +1。空批次成功但不改变 generation。

## 所有权与并发

- 引擎用一把 `sync.RWMutex`：`Apply` 写锁，`Reachable`/`Snapshot` 读锁；`Reachable` 在读锁内对当前一致快照做 DFS。
- `Snapshot` 对节点和边（按 `From` 再 `To`）稳定排序，并拷贝到新切片返回；`Coordinator.Decisions` 同样返回拷贝，调用方修改不影响内部状态。
- `Policy` 用独立的 `sync.RWMutex`：`ReplaceActors` 先校验再整体换 map（原子替换，失败保留旧表）；`Authorize` 只读锁，拒绝时不触碰核心状态。
- `Coordinator` 用独立 `sync.Mutex` 保证审计序号单调连续，授权 → 引擎 → 记录之间不持锁，避免层间锁耦合。

## 复杂度

设 V/E 为当前节点/边数，k 为批内操作数：

- `Apply`：结构校验 O(k·名称长)；候选拷贝 O(V+E)；每操作 O(1) 均摊，加边环检测 O(V+E)；容量检查 O(1)。
- `Reachable`：O(V+E)。`Snapshot`：O(V log V + E log E)。
- `Authorize`/`ReplaceActors`：O(1)/O(演员数)；`Decisions`：O(日志长)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
