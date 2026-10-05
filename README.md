# controlgraph133

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。实现见 `controlgraph133/` 下三个协同的生产文件。

## 架构

- **状态引擎（`trustgraph.go`）**：`Graph` 持有节点/边事务性数据与快照，所有公开方法经 `sync.RWMutex` 保护。
- **策略层（`policy.go`）**：`Policy` 独立加锁，维护可原子替换的 actor 白名单（`ReplaceActors` 整体换入新集合）与单批操作数上限；`Authorize` 只读策略，不触碰核心状态。
- **协调层（`coordinator.go`）**：`Coordinator` 串行化准入：先 `Authorize`，拒绝则直接记录审计并返回 `ErrDenied`（不读取/修改核心状态）；通过后委托 `Graph.Apply`，为成功、拒绝与引擎失败均分配连续递增的审计序号。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From,To}` 为可比较键），O(1) 查重/删除。
- 白名单：`map[string]struct{}`，替换时整体构建后原子换入。

## 候选事务

`Apply` 先对全部操作做完整结构校验（kind、名称字符集与字节上限、多余字段），不读状态；随后在节点/边索引的**副本**上顺序执行语义检查（`ErrExists`/`ErrNotFound`/`ErrCycle`），仅在批次末检查最终节点/边容量。任何失败直接丢弃副本，整体回滚，核心状态与 generation 不变；成功才一次性换入并使 generation 恰好 +1（空批次不变）。加边前用以边索引为邻接表的 DFS 检查 `To→From` 可达性以阻止有向环；删除节点级联删除关联边。

## 所有权

所有返回切片（`Snapshot.Nodes/Edges`、`Coordinator.Decisions`）均为新分配的副本，调用方修改不影响内部状态；`Snapshot` 对节点与边稳定排序（节点按字典序，边按 `(From,To)`）。

## 复杂度

- 结构校验：O(B·L)，B 为批内操作数，L 为名称长度。
- 候选事务：复制索引 O(N+E)；每条 AddEdge 的环检查 O(V+E)；容量检查 O(1)。
- `Reachable`：O(V+E)，在读锁下基于当前一致快照。
- `Snapshot`：O(N log N + E log E)。
- `Authorize`/`ReplaceActors`：O(1) / O(A)；`Decisions`：O(D)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
