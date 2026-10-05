# ownershipgraph

并发安全的内存型所有权依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
图状态由四份互为冗余的索引组成，全部封装在不可导出的 `state` 中：
- `nodes map[string]struct{}`：节点存在性集合，O(1) 查询。
- `edges map[Edge]struct{}`：边存在性集合，O(1) 判重。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性遍历与删节点级联。
- `in map[string]map[string]struct{}`：入边邻接表，使删节点时无需全图扫描即可找到入边。

### 候选事务（candidate transaction）
`Apply` 先对整个批次做纯结构校验（kind 合法、多余字段为空、名称字符集与字节上限），此阶段不读任何图状态。校验通过后，在写锁内把当前 `state` 深拷贝为候选状态，按序在候选上执行全部操作；任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败，直接丢弃候选，原状态零改动。只有全部成功且最终节点数/边数均在容量内时，才用候选整体替换当前状态并将 generation 加一。空批次不触碰写锁、不改变 generation。

### 所有权与并发
- 一把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此读操作永远看到一致的快照。
- `Snapshot` 返回的切片是新分配的副本，节点按字典序、边按 (From, To) 稳定排序；调用方修改返回值不影响内部状态。
- 环检测在候选状态的 `out` 邻接表上做 DFS：加边 `u→v` 前检查 `u` 是否可从 `v` 到达（含自环）。

### 复杂度
设批次含 B 个操作，图中有 N 个节点、E 条边：
- 结构校验：O(B · L)，L 为名称长度上限。
- 候选拷贝：O(N + E)。
- AddNode/DeleteEdge：均摊 O(1)；DeleteNode：O(deg)，deg 为该节点关联边数。
- AddEdge：O(1) 判重 + O(N + E) 环检测（最坏情况 DFS 全图）。
- 容量检查：O(1)（map 长度）。
- Reachable：O(N + E) BFS/DFS。
- Snapshot：O(N log N + E log E) 排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
