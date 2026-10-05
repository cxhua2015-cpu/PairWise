# dependencygraph

并发安全的内存型有向组件依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量在批次末检查、失败整体回滚。

## 设计

### 索引

图内维护三份索引，均由同一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `out map[string]map[string]struct{}`：出边邻接表（from → to 集合）。
- `in map[string]map[string]struct{}`：入边邻接表（to → from 集合）。

`out`/`in` 双邻接表使 `DeleteNode` 能 O(度数) 级联删除两侧关联边，
边存在性判断与环检测的 DFS 均为 O(1) 每步探测。`edgeCount` 计数器避免
容量检查时遍历。

### 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：先完整校验所有 op 的 kind、名称字符集（`[a-z0-9_-]`、
   非空、≤ `MaxNameBytes`）与字段约束（节点 op 不得带 `To`，边 op 两端
   必填），任何错误返回 `ErrInvalidInput`，此阶段不读取图状态。
2. **候选应用**：克隆 `nodes`/`out`/`in` 为候选副本，在其上顺序应用
   全部 op（存在性、缺失、环检测即时报错）；最后统一检查
   `len(nodes) ≤ MaxNodes` 与 `edgeCount ≤ MaxEdges`。任何失败直接丢弃
   候选副本即完成回滚；成功则整体换入并将 `generation` 加一。空批次不
   变更 generation。

### 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，
  因此 `Reachable` 与 `Snapshot` 总是观察到同一代的一致状态。
- 返回的 `Snapshot`（含 `Nodes`/`Edges` 切片）是新分配的副本，与内部
  状态完全隔离，调用方可自由修改；节点按字典序、边按 `(From, To)`
  稳定排序。
- `Graph` 不可复制；通过 `New` 返回的指针共享使用。

### 复杂度

设 N 为节点数、E 为边数、K 为批次内 op 数：

- `New`：O(1)。
- `Apply`：克隆候选 O(N + E)；每个 `AddEdge` 的环检测为一次 DFS，
  O(N + E)；整体 O(N + E + K·(N + E))，最坏 O(K·(N + E))。
- `Reachable`：一次 DFS，O(N + E)。
- `Snapshot`：收集 O(N + E)，排序 O(N log N + E log E)。
- 空间：O(N + E)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
