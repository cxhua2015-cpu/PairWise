# topologygraph333

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

`Graph` 持有四份冗余索引，全部在单个 `sync.RWMutex` 保护下：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边存在性，O(1) 判重。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与环检测。
- `in  map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 级联删除为 O(deg)。

## 候选事务（candidate transaction）

`Apply` 分三个阶段：

1. **结构校验**：仅校验 kind、名称字符集（`[a-z0-9-_]`、非空、≤ `MaxNameBytes`）
   与多余字段，不读取任何状态；失败返回 `ErrInvalidInput`。
2. **候选执行**：在节点/边/邻接表的私有副本上顺序应用全部操作；
   任何 `ErrExists` / `ErrNotFound` / `ErrCycle` 直接丢弃副本，原状态不受影响。
3. **容量检查与提交**：仅在批次末比较 `MaxNodes` / `MaxEdges`，超限返回
   `ErrCapacity` 并回滚；成功则原子换入副本，`generation` 恰好加一
   （空批次不增加）。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable` / `Snapshot` 取读锁，
  因此 `Reachable` 总是基于某一个一致快照。
- 返回的切片（`Snapshot.Nodes` / `Snapshot.Edges`）均为新建并排序后的副本，
  调用方修改不会影响内部状态；`Snapshot` 对节点字典序、边按 `(From, To)` 稳定排序。
- 提交采用整图换入，旧索引不可变地被读锁持有者使用后由 GC 回收，无共享可变状态。

## 复杂度

设 V 为节点数、E 为边数、k 为批次数：

- `New`：O(1)。
- `Apply`：拷贝 O(V+E)，每操作均摊 O(1)，`AddEdge` 环检测 O(V+E)，
  总计 O(V + E + k·(V+E)) 上界；空间 O(V+E)。
- `Reachable`：O(V+E) DFS。
- `Snapshot`：O(V log V + E log E) 排序。
- `DeleteNode` 级联：O(deg(node))。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
