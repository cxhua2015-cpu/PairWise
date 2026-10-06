# topologygraph203

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引

`Graph` 内部维护四类索引，均在同一个 `sync.RWMutex` 保护下：

- `nodes map[string]struct{}`：节点存在性集合，O(1) 查重。
- `edges map[Edge]struct{}`：边集合，键为 `{From, To}`，O(1) 判重/删除。
- `out map[string]map[string]struct{}`：正向邻接表，用于环检测与 `Reachable` 的图遍历。
- `in map[string]map[string]struct{}`：反向邻接表，使 `DeleteNode` 能 O(度数) 级联删除入边，无需全表扫描。

### 候选事务（candidate transaction）

`Apply` 分两阶段保证原子性：

1. **结构校验**：在读取任何状态之前，先完整校验所有 op 的 kind、名称字符集（非空 ASCII 小写字母/数字/`-`/`_`，且不超过 `MaxNameBytes`）与字段约束（节点 op 不得带 `To`）。任一失败返回 `ErrInvalidInput`，不产生副作用。
2. **候选副本执行**：对 `nodes`/`edges`/`out`/`in` 做浅拷贝（邻接表内层 map 也复制），在副本上顺序应用全部 op，期间检查 `ErrExists`/`ErrNotFound`/`ErrCycle`；全部成功后只在**批次末**检查节点/边容量（`ErrCapacity`）。任何一步失败直接丢弃副本——回滚是“不提交”，因此天然无部分写入。成功时一次性用副本替换原索引，`generation` 恰好加一；空批次不加。

### 所有权

- 所有公开方法（`Apply`/`Reachable`/`Snapshot`）可并发调用：写操作取互斥锁，读操作取读锁。
- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片，按节点名、边 `(From, To)` 字典序稳定排序，调用方修改返回值不影响内部状态。
- 环检测与可达性共用同一 DFS：加边 `u→v` 前检查 `v` 是否可达 `u`（节点视为可达自身，故自环也是环）。

### 复杂度

设批次含 `k` 个 op，图有 `n` 个节点、`m` 条边：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(n+m)；每个 `AddEdge` 的环检测为一次 DFS，O(n+m)；容量检查 O(1)。整体 O(n + m + k·(n+m))。
- `Reachable`：单次 DFS，O(n+m)。
- `Snapshot`：收集 O(n+m)，排序 O(n log n + m log m)。
- 空间：O(n+m)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
