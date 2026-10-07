# topologygraph393

并发安全的内存型有向无环“控制拓扑图”，仅依赖标准库（Go 1.22+）。
原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

图内部维护三份冗余索引，用一次加锁的写路径保持一致：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判断重复边 / 缺失边。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS、
  环检测以及 `DeleteNode` 的级联删除。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在不读任何状态的情况下校验整个批次——未知 kind、
   节点 op 携带多余 `To` 字段、空名 / 非法字符 / 超长名字都返回
   `ErrInvalidInput`，此时不加锁、不触碰状态。
2. **候选执行**：在写锁内把三份索引浅拷贝为候选副本，按顺序在副本上
   应用每个 op（`ErrExists` / `ErrNotFound` / `ErrCycle` 任一失败即
   丢弃副本，天然回滚）；最后仅在候选终态上检查
   `MaxNodes` / `MaxEdges`，超限返回 `ErrCapacity` 并丢弃。
   全部成功才把候选副本整体换入，`generation` 恰好加一。
   空批次不修改状态、不增加 generation。

环检测：加入 `from -> to` 会成环当且仅当 `to` 在候选图中已可达 `from`
（含 `from == to` 自环），用邻接表做一次 DFS 判定。

## 所有权与并发

- 所有公开方法并发安全：写路径持 `sync.Mutex`，`Reachable` / `Snapshot`
  走 `sync.RWMutex` 读锁，读到的是同一时刻的一致状态。
- `Snapshot` 返回的 `Nodes`（按字典序）与 `Edges`（按 `(From, To)`）为
  稳定排序的新切片，调用方修改返回值不影响内部状态。
- `Options` 在 `New` 时拷贝保存，之后只读。

## 复杂度

设批次含 B 个 op，图有 N 个节点、E 条边：

- `Apply`：候选拷贝 O(N + E)；每个 `AddEdge` 的环检测 O(N + E)；
  `DeleteNode` 级联 O(出度 + 关联边)；容量检查 O(1)。整批
  O(N + E + B·(N + E))，失败时额外状态零残留。
- `Reachable`：O(N + E) DFS。
- `Snapshot`：O(N log N + E log E) 排序。
- 空间：O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
