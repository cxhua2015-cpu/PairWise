# controlgraph098

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- `nodes: map[string]struct{}` —— 节点集合，O(1) 存在性判断。
- `edges: map[Edge]struct{}` —— 边集合，O(1) 判重/删除。
- `out / in: map[string]map[string]struct{}` —— 出边/入边邻接表，分别支撑环检测的正向 BFS 与删节点时的级联删边。

**候选事务（candidate transaction）**
`Apply` 先做整批结构校验（kind、名称字符集与字节上限、多余字段），不读状态；随后克隆当前四张索引为候选状态，在候选上顺序执行全部操作，最后才检查节点/边容量上限。任一步失败直接丢弃候选，原状态零改动，实现整体回滚；成功则原子交换索引指针并将 generation 加一（空批次不增加）。

**所有权与并发**
- 所有公开方法经 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，可并发调用。
- `Snapshot` 返回新建且排序后的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态；`Reachable` 在读锁内基于当前一致快照做 BFS。
- 环检测：加边 `u→v` 前检查候选状态中 `v` 是否可达 `u`（含 `u == v` 自环）。

**复杂度**（N 节点、E 边、B 批次大小）
- `Apply`：克隆 O(N+E)，每操作均摊 O(1)，每次加边环检测 O(N+E)，合计 O(N+E+B·(N+E))。
- `Reachable`：O(N+E)。`Snapshot`：O(N log N + E log E)。
- `New`：O(1)；Options 中容量与名称上限必须为正，否则 `ErrInvalidOptions`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
