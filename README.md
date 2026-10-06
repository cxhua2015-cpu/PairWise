# topologygraph208

并发安全的内存型有向控制拓扑图，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计

### 索引

`Graph` 持有四份始终一致的索引：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[edgeKey]struct{}`：边去重与存在性，O(1) 查询。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与环检测。
- `in  map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(度数) 级联删除关联边。

### 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前校验全部 Op 的 kind、字段与名称合法性，任一失败返回 `ErrInvalidInput`，不触碰状态。
2. **候选执行**：把已提交状态深拷贝为 candidate，在其上顺序执行所有 Op（语义错误如 `ErrExists`/`ErrNotFound`/`ErrCycle` 在此检出），最后统一检查节点/边容量（`ErrCapacity`）。任何失败直接丢弃 candidate，已提交状态零改动，天然回滚；全部成功才整体换入并将 `generation` 加一。空批次成功但不改变 generation。

### 所有权与并发

- 所有公开方法由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 总是基于当前一致快照。
- 状态不外借：`Snapshot` 返回新建且按节点字典序、边 `(From, To)` 字典序稳定排序的切片，调用方修改返回值不影响内部状态；candidate 深拷贝保证失败批次与提交后的图不共享内存。

### 复杂度

- `AddNode`/`DeleteEdge`：O(1)。
- `DeleteNode`：O(度数)，级联删除关联边。
- `AddEdge`：O(V+E)，通过出边 DFS 检查 `to` 是否可达 `from` 以阻止有向环（含自环）。
- `Apply`：结构校验 O(|Ops|·L)（L 为名称长度），候选克隆 O(V+E)，批次执行逐 Op 累加，容量检查 O(1)。
- `Reachable`：O(V+E) DFS，节点自身可达自身。
- `Snapshot`：O(V log V + E log E) 排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
