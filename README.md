# subscriptiongraph

并发安全的内存型订阅依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes`：`map[string]struct{}`，节点存在性 O(1)。
- `edges`：`map[Edge]struct{}`，边存在性 O(1)。
- `out` / `in`：`map[string]map[string]struct{}` 正/反邻接表，用于可达性遍历、成环检测和删节点时的级联删边；空邻接集合即时回收，避免内存泄漏。

### 候选事务（原子批次）
`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集与字节上限、节点操作不得携带多余字段），不读取图状态；随后顺序应用每个操作，每步记录一个逆操作（undo）。任一步失败或批次末容量（`MaxNodes`/`MaxEdges`）超限，即按逆序回放 undo 整体回滚，图状态与 `generation` 保持不变。空批次为成功空操作且不增加 `generation`；非空成功批次 `generation` 恰好加一。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此 `Reachable` 总是基于某一一致快照。
- `Snapshot` 返回的 `Nodes`/`Edges` 为新分配切片，节点按字典序、边按 `(From, To)` 稳定排序；调用方修改返回值不影响内部状态。
- 加边前以 DFS 检查 `to → from` 是否已可达（含自环），可达则返回 `ErrCycle`；删除节点级联删除其全部出入边。

### 复杂度
设批次长度 B、节点数 N、边数 E。
- `Apply`：结构校验 O(B·L)（L 为名称长度）；加边成环检测最坏 O(N+E)；删节点级联 O(度数)；容量检查 O(1)；整体回滚代价与已应用操作同阶。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
