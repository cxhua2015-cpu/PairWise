# topologygraph358

并发安全的内存型有向无环“控制拓扑图 358”，仅依赖 Go 标准库（Go 1.22+）。
原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重。
- `out` / `in map[string]map[string]struct{}`：出边 / 入边邻接表，
  用于环检测、可达性遍历与删除节点时的级联清理。

## 候选事务

`Apply` 分两阶段：先对整个批次做完整结构校验（kind、名称字符集与字节上限、
多余字段），不读取状态；随后在写锁内把 `nodes/edges/out/in` 浅拷贝为候选副本，
在副本上顺序执行全部操作。任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）
或批次末容量检查失败（`ErrCapacity`）都直接丢弃副本，实现整体回滚；
全部成功才一次性替换内部状态并将 `generation` 加一（空批次不变）。

## 所有权与并发

- 单个 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，
  因此 `Reachable` 总是基于当前一致快照。
- `Snapshot` 返回的节点与边切片均为新分配并稳定排序（节点按字典序，边按
  `(From, To)` 字典序），调用方修改返回值不影响内部状态。
- 名称仅允许非空 ASCII 小写字母、数字、`-`、`_`，且不超过 `MaxNameBytes`。

## 复杂度

设批次含 k 个操作，图中 V 个节点、E 条边：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(V+E)；
  每个 AddEdge 的环检测为一次 DFS，O(V+E)；容量检查 O(1)。
- `Reachable`：一次 DFS，O(V+E)。
- `Snapshot`：O(V+E) 收集 + O(V log V + E log E) 排序。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
