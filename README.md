# rolloutgraph

并发安全的内存型有向发布依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引

`Graph` 内部维护三张索引，由一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}`：节点集合。
- `out map[string]map[string]struct{}`：正向邻接表（from → to 集合），用于环检测与 `Reachable` 的 DFS。
- `in  map[string]map[string]struct{}`：反向邻接表（to → from 集合），使 `DeleteNode` 能级联删除入边而无需扫描全图。

另维护 `nEdges` 计数与单调递增的 `generation`。读路径（`Reachable`、`Snapshot`）走 `RLock`，可并发；写路径（非空 `Apply`）走 `Lock`，串行提交。

### 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：对整个批次做纯结构检查（kind 合法、节点操作不带 `To`、名称字符集与字节上限），不读取任何图状态；任一 op 非法即返回 `ErrInvalidInput`。
2. **候选执行**：把三张索引深拷贝为候选副本，在副本上按序执行全部 op（存在性、环检测、级联删除都在副本上进行），最后才在副本上检查节点/边容量。任何一步失败直接丢弃副本——原图状态与 generation 完全不变，天然实现整体回滚。全部成功才将副本原子换入并把 `generation` 加一（空批次不加）。

### 所有权

- 所有公开方法并发安全；返回值（`Result`、`Snapshot` 及其切片）均为新分配的副本，调用方修改不会影响内部状态。
- 传入的 `Batch`/`Op` 只按值读取，实现不保留其引用。
- `Snapshot` 的 `Nodes` 按字典序、`Edges` 按 `(From, To)` 稳定排序。

### 复杂度

设 N=节点数，E=边数，B=批次内 op 数，D=单节点关联边数：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；每个 `AddEdge` 的环检测为一次 DFS，O(N+E)；`DeleteNode` 级联 O(D)；容量检查 O(1)。整体 O(N + E + B·(N+E))，回滚零额外成本。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
