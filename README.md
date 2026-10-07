# topologygraph323

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 做 DFS；删除节点时同步清理其出边与所有入边。

### 候选事务（candidate transaction）
`Apply` 先做整批结构校验（kind 合法、名称字符集/长度、节点操作不得带 `To`），不通过返回 `ErrInvalidInput` 且不触碰状态。校验通过后，在写锁内把 `nodes/edges/out` 深拷贝为候选状态，按序应用全部操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败，直接丢弃候选状态，实现整体回滚。全部成功才一次性换入候选状态并将 `generation` 加一；空批次成功但 generation 不变。

### 所有权与并发
- 所有公开方法经 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写均可并发调用。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From,To)` 稳定排序），调用方修改返回值不影响内部状态；图不保留调用方传入的任何切片。
- 环检测在候选状态的邻接表上做 DFS（`to` 是否可达 `from`，含自环），保证加边后图仍为 DAG。

### 复杂度
设批次长 B、节点数 N、边数 E：
- `Apply`：结构校验 O(B·名称长度)；候选拷贝 O(N+E)；加边环检测 O(N+E)；容量检查 O(1)；整体 O(N+E+B·(N+E)) 上界。
- `Reachable`：O(N+E) DFS。
- `Snapshot`：O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证
`go test ./...`、`go test -race ./...`、`go run ./cmd/demo`。
