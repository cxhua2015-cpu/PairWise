# topologygraph328

并发安全的内存型有向无环控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：状态为不可变快照式结构 `state`，含 `nodes map[string]struct{}`、`edges map[Edge]struct{}` 及出边邻接表 `out map[string]map[string]struct{}`。点/边存在性 O(1)，可达性沿邻接表 DFS。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、未用字段必须为空、名称字符集与字节上限），不读状态；随后在 `state.clone()` 的候选副本上顺序执行操作（存在性、环检测：加边 `u→v` 前检查 `v` 可达 `u`），全部成功且**批次末**节点/边容量不超限才原子换入并令 generation 加一；任一步失败直接丢弃候选，原状态零成本回滚。空批次不改变 generation。
- **所有权**：所有公开方法返回的切片/快照均为新建拷贝，与内部状态隔离；调用方修改返回值不影响图。
- **并发**：单把 `sync.RWMutex` 保护状态指针与 generation；`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，读操作看到的是同一份一致快照。
- **复杂度**：批次结构校验 O(批大小)；候选克隆 O(N+E)；每次加边环检测 O(N+E)；容量检查 O(1)；`Snapshot` 排序 O(N log N + E log E)；`Reachable` O(N+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
