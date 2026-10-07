# topologygraph438

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删节点级联删边，容量只在批次末检查、失败整体回滚。

## 多文件架构

- `trustgraph.go` — 核心事务引擎：`Graph`、`New`、`Apply`、`Reachable`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（kind 合法、名称字符集与字节上限、无多余字段、禁止自环），不读取任何状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回同一逻辑时刻的 generation / 节点数 / 边数。
- `clone.go` — 深拷贝：`Clone` 保留逻辑时钟（generation），并完全隔离所有权。
- `preview.go` — 事务预演：`Preview` 在一次线性化快照上复用完整 `Apply` 语义，返回候选 `Result` / `Snapshot` / `Stats`；原对象状态、generation 与逻辑时钟不变，错误及优先级与同状态 `Apply` 完全一致，失败时全部返回零值。

## 索引

- `nodes map[string]struct{}` — 节点存在性 O(1)。
- `edges map[Edge]struct{}` — 边存在性 O(1)。
- `adj map[string]map[string]struct{}` — 出边邻接表，供环检测与 `Reachable` 的 DFS 使用。

## 候选事务

`Apply` 在写锁内先把三个索引浅拷贝为候选状态，在候选上顺序执行全部操作（任一失败即丢弃候选，整体回滚，原状态与 generation 不变）；仅当全部操作成功且批次末容量（`MaxNodes` / `MaxEdges`）检查通过时才一次性换入候选并将 generation 加一。空批次不改变 generation。`Preview` 通过 `Clone` 取得一致快照后在其上运行同一候选事务，因此与原对象完全隔离。

## 所有权

所有公开方法返回的切片（`Snapshot.Nodes` / `Snapshot.Edges`）均为新建副本，调用方修改不会影响内部状态；`Clone` 逐层复制 map，克隆体与原对象互不影响。所有公开方法均可并发调用（`sync.RWMutex`：写路径独占，读路径共享）。

## 复杂度

- `ValidateBatch`：O(B·L)，B 为批大小，L 为名称长度。
- `Apply`：O(N + E + B·(V + E))——候选拷贝 O(N+E)，每次 `AddEdge` 环检测为一次 DFS O(V+E)。
- `Reachable`：O(V + E)。`Snapshot`：O(N log N + E log E)（稳定排序）。
- `Stats`：O(1)。`Clone` / `Preview`：O(N + E) 加一次候选事务。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
