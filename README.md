# topologygraph233

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 架构

实现按职责拆分到四个联动文件，共享同一套结构语义：

- `trustgraph.go` — 核心事务引擎：`Apply`、索引维护、环检测、`Reachable`、`Snapshot`。
- `validation.go` — 无副作用的批次预检 `ValidateBatch`：只读取 `Options`，不触碰图状态；
  `Apply` 在加锁前调用同一函数，保证预检与事务语义一致。
- `stats.go` — `Stats` 在读锁下返回线性一致的 `{Generation, Nodes, Edges}` 摘要。
- `clone.go` — `Clone` 在读锁下深拷贝全部索引与逻辑时钟（generation），所有权完全隔离。

## 索引

- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性/去重 O(1)。
- `out` / `in map[string]map[string]struct{}`：出/入邻接表，用于 DFS 环检测、
  `Reachable` 以及 `DeleteNode` 时 O(度数) 的级联删除；空桶即时回收。

## 候选事务与回滚

`Apply` 先做完整结构校验（未知 kind、非法名称、多余字段、自环 → `ErrInvalidInput`），
再在写锁内以“候选事务”方式顺序应用：每个变更记录一条逆操作（undo），
任一步失败（`ErrExists` / `ErrNotFound` / `ErrCycle`）或批次末容量超限
（`ErrCapacity`）时按逆序回放 undo，状态整体回滚。非空成功批次 generation 恰好 +1，
空批次不变。环检测在加边前用候选状态的邻接表做 `To ⇝ From` DFS。

## 所有权与并发

所有公开方法经 `sync.RWMutex` 串行化：`Apply` 持写锁，`Reachable` / `Snapshot` /
`Stats` / `Clone` 持读锁，因此 `Reachable` 与 `Stats` 总是观察一致快照。
`Snapshot` 返回新分配的、按节点名与 `(From, To)` 稳定排序的切片；`Clone` 逐层
复制 map，调用方与克隆体互不影响。名称仅允许非空 ASCII 小写字母、数字、
`-`、`_`，长度不超过 `Options.MaxNameBytes`。

## 复杂度

- `ValidateBatch`：O(批次字节数)，零状态访问。
- `Apply`：O(Σ 每步代价)；加边含一次 DFS O(V+E)，删节点 O(度数)，容量检查 O(1)。
- `Reachable`：O(V+E)。`Snapshot`：O(V log V + E log E)。`Stats`：O(1)。`Clone`：O(V+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
