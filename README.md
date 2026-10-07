# topologygraph333

并发安全的内存型有向无环“控制拓扑图”，仅依赖 Go 标准库（Go 1.22+）。公开 API 与错误值见 `topologygraph333/trustgraph.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

**索引**
- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`，以 `{From, To}` 结构体为键，O(1) 去重与删除。
- 未维护邻接表；可达性检查（含 AddEdge 的成环检测）按需从边集构建临时邻接表做 DFS。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind 合法、名称字符集/长度、无多余字段），不读取任何状态。
- 校验通过后持写锁，在“候选索引”上顺序执行各操作；候选索引采用 clone-on-write：首个修改操作时才复制节点/边映射，只读或失败批次零拷贝。
- 任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时，直接丢弃候选索引，提交状态不变，实现整体回滚。
- 成功且非空的批次提交候选索引并将 `generation` 加一；空批次不改变 generation。

**所有权与并发**
- 单个 `sync.RWMutex` 保护全部内部状态：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 总是基于某一已提交批次的一致快照。
- `Snapshot` 返回新分配的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方对返回值的修改不影响内部状态。
- 图在任意已提交状态下始终保持无环：成环检测在写锁内完成，并发加边不会引入环。

**复杂度**（N=节点数，E=边数，B=批次操作数）
- `Apply`：结构校验 O(B·名称长度)；执行阶段 AddNode/AddEdge/DeleteEdge 均摊 O(1)，DeleteNode O(E)（级联删除关联边），每次 AddEdge 成环检测 O(N+E)；批次末容量检查 O(1)。候选索引克隆 O(N+E)，每批次至多一次。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
