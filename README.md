# topologygraph373

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点存在性集合，O(1) 查找。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 可比较，直接作键），O(1) 判重。
- `adj map[string]map[string]struct{}`：出边邻接表，与 `edges` 同步维护，
  供可达性 DFS 与删节点级联使用；反向边在删节点时通过遍历邻接表定位。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：先对全部 `Op` 做纯结构检查（未知 kind、多余字段、非法名称），
   不读取任何图状态，失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把 `nodes`/`edges`/`adj` 深拷贝为候选副本，按序应用
   全部操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本。
   全部成功后在批次末检查 `MaxNodes`/`MaxEdges`，超限返回 `ErrCapacity` 并丢弃。
   只有完全成功才把候选副本整体换入并令 `generation` 恰好 +1（空批次不变）。

由于失败路径只丢弃副本、从不触碰 live 状态，回滚是零成本的，无需 undo 日志。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot`
  持 `RWMutex` 读锁，可并行执行。
- `Snapshot` 返回的切片是新分配的排序副本（节点按字典序、边按 `(From,To)` 排序），
  调用方修改返回值不影响内部状态；`Reachable` 在读锁内遍历，看到一致快照。
- 名称仅允许非空 ASCII 小写字母、数字、`-`、`_`，且字节长度 ≤ `MaxNameBytes`。

## 复杂度

设 N=节点数，E=边数，B=批次内操作数：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；
  每条 `AddEdge` 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E) DFS。
- `Snapshot`：O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
