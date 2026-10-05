# controlgraph188

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- `nodes: map[string]struct{}` 存节点集合。
- `out: map[from]map[to]struct{}` 与 `in: map[to]map[from]struct{}` 为正向/反向邻接索引，二者始终互逆；`edges` 计数器避免 O(E) 统计。删除节点时借助 `in`/`out` 以 O(关联度) 级联删除关联边。

**候选事务（原子批次）**
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限、多余字段），不读取任何图状态，失败返回 `ErrInvalidInput`。
- 校验通过后在写锁内把 `nodes`/`out`/`in` 深拷贝为候选状态，按序在候选上执行各操作（环检测在候选图上做 DFS）；任一操作失败或批次末容量（`MaxNodes`/`MaxEdges`）超限即丢弃候选，整体回滚，generation 不变。
- 成功后用候选整体替换正式状态，非空批次 generation 恰好 +1；空批次为无操作，generation 不变。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁（`sync.RWMutex`）。
- `Reachable` 在读锁内对当前一致快照做 DFS，不会观察到批次中间态。
- `Snapshot` 每次新建切片并对节点（字典序）与边（From 再 To）稳定排序，返回的切片与内部状态完全隔离，调用方可自由修改。

**复杂度**（V 节点数，E 边数，B 批次数）
- `Apply`：结构校验 O(B·名称长度)；候选拷贝 O(V+E)；每个 AddEdge 环检测 O(V+E)；整体 O(V+E+B·(V+E))。
- `Reachable`：O(V+E)。
- `Snapshot`：O(V log V + E log E)。
- 空间：O(V+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
