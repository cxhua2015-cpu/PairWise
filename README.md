# topologygraph203

并发安全的内存型有向无环“控制拓扑图”，仅依赖 Go 标准库（Go 1.22+）。
语义详见 `SPEC.md`：原子批次（AddNode/DeleteNode/AddEdge/DeleteEdge）、加边防环、
删节点级联删边、容量在批次末检查并整体回滚、`Reachable` 一致快照、`Snapshot` 稳定排序。

## 索引结构

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From, To}` 为可比较键），O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 的 DFS/BFS 使用；
  删除最后一条出边时回收空内层 map，避免泄漏。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在不读取图状态的前提下校验全部 op 的 kind 与名称合法性
   （非空、仅 `[a-z0-9-_]`、不超过 `MaxNameBytes`），任何失败直接返回 `ErrInvalidInput`。
2. **执行 + 撤销日志**：在写锁内逐条应用 op，同时记录逆操作（undo log）。
   任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败
   （`ErrCapacity`）时，按逆序回放 undo log 完整回滚，图状态与 generation 保持不变。
   非空成功批次 generation 恰好 +1；空批次不触碰写锁、不改变 generation。

## 所有权与并发

- 单个 `sync.RWMutex` 保护全部内部状态：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，
  因此读者永远观察到一致的快照，写者之间串行化。
- `Snapshot` 返回的 `Nodes`/`Edges` 均为新分配的排序副本，调用方修改返回值不会影响内部状态。
- 不暴露任何内部 map 或切片引用，所有权始终留在 `Graph` 内。

## 复杂度

设批次含 k 个 op，图含 n 个节点、m 条边：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；执行阶段每个 AddEdge 的环检测为一次
  O(n+m) DFS，整体 O(k·(n+m))；容量检查 O(1)。
- `DeleteNode`：O(m) 扫描边集做级联删除。
- `Reachable`：O(n+m) BFS/DFS。
- `Snapshot`：O(n log n + m log m) 排序，节点按字典序、边按 (From, To) 字典序。
- 空间：O(n+m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
