# controlgraph098

并发安全的内存型“控制面依赖图 098”（Go 1.22+，仅标准库）。实现见 `controlgraph098/trustgraph.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计要点

- **索引**：图状态由两个哈希索引承载——`nodes map[string]struct{}`（节点存在性 O(1)）与 `edges map[Edge]struct{}`（边存在性 O(1)）。未单独维护邻接表；可达性与删除节点的级联删边直接扫描边集，换取实现的简洁与候选事务复制的低成本。
- **候选事务**：`Apply` 分三个阶段。阶段一对全部操作做纯结构校验（kind 合法、名称字符集/长度、无多余字段），不读取任何状态；阶段二在节点集与边集的**私有副本**（候选事务）上顺序执行操作，任何 `ErrExists`/`ErrNotFound`/`ErrCycle` 直接丢弃副本返回，天然整体回滚；阶段三才检查最终节点/边容量（`ErrCapacity`），同样只影响副本。全部成功才把副本一次性交换进图并将 generation 加一；空批次不改变 generation。
- **所有权**：所有公开方法返回的数据（`Result`、`Snapshot` 的切片）均为新建副本，与内部状态完全隔离，调用方可自由修改。`Graph` 内部由一把 `sync.RWMutex` 保护：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此所有公开方法可并发调用，`Reachable` 与 `Snapshot` 总是观察到同一个一致的已提交状态。
- **环检测**：加边 `from→to` 前在候选边集上检查 `to` 是否可达 `from`（含 `from == to` 的自环），可达则返回 `ErrCycle`，保证图始终是有向无环图。
- **排序**：`Snapshot` 的节点按字典序、边按 `(From, To)` 字典序稳定排序。

## 复杂度

设 N 为节点数、E 为边数、K 为批内操作数。

- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选复制 O(N+E)；每个 `AddEdge` 的环检测为 O(E) 的 DFS，`DeleteNode` 级联为 O(E)；整体 O(N + E·K)。
- `Reachable`：O(E)（DFS/BFS 扫描边集）。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)，候选事务期间额外一份 O(N+E) 副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
