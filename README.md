# controlgraph148

并发安全的内存型“控制面依赖图 148”（Go 1.22+，仅标准库）。原子批次支持
AddNode/DeleteNode/AddEdge/DeleteEdge，加边阻止有向环，删除节点级联删除关联边，
容量只在批次末检查，失败整体回滚。

## 三层架构

- `trustgraph.go`（状态引擎）：持有事务性数据与快照。单把 `sync.RWMutex` 保护
  `nodes`/`edges` 两个 map 索引与 `generation`；`Apply` 独占写锁，`Reachable`/
  `Snapshot` 使用读锁获得一致快照。
- `policy.go`（策略层）：独立同步的准入配置。actor 白名单存于 `atomic.Value`
  中的不可变 map，`ReplaceActors` 原子整体替换，读者无锁；`Authorize` 同时检查
  单批操作数上限。策略拒绝不触碰核心状态。
- `coordinator.go`（协调层）：先 `Authorize` 再委托状态引擎；无论成功、拒绝或
  引擎失败，都在互斥锁下分配连续递增的审计序号。`Decisions` 返回拷贝切片，
  不别名内部存储。

## 索引与候选事务

- 索引：`nodes map[string]struct{}` 与 `edges map[Edge]struct{}` 提供 O(1) 存在性
  判定；未维护邻接表，连通性通过边集扫描 DFS 完成。
- 候选事务：`Apply` 先做整批结构校验（kind、名称字符集与字节上限、多余字段），
  再在节点/边 map 的拷贝上顺序应用全部操作；任一步失败直接丢弃拷贝，原状态
  不变。容量（MaxNodes/MaxEdges）仅在批次末对候选结果检查，超限返回
  `ErrCapacity` 并整体回滚。仅非空成功批次使 generation 恰好 +1。

## 所有权

所有公开方法返回的切片（`Snapshot.Nodes/Edges`、`Coordinator.Decisions`）均为
新分配的拷贝，调用方修改不会影响内部状态；`Policy` 的白名单在替换时重建，
旧 map 永不原地修改。

## 复杂度

- `Apply`：结构校验 O(L)，L 为操作名总字节；候选复制 O(N+E)；环检测每次加边
  O(N+E)；整批 O(N+E+Σops·(N+E))。
- `Reachable`：O(N+E) DFS；`Snapshot`：O(N log N + E log E) 稳定排序。
- `Authorize`/`ReplaceActors`：O(1) / O(A)，A 为 actor 数。
- `Coordinator.Apply`：在引擎开销上仅加 O(1) 审计追加；`Decisions` O(D) 拷贝。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
