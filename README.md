# controlgraph118

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes`: `map[string]struct{}` 节点集合，O(1) 存在性判断。
- `edges`: `map[Edge]struct{}` 边集合，O(1) 判重。
- `out` / `in`: 出边与入边邻接表（`map[string]map[string]struct{}`），用于环检测的 DFS、`Reachable` 遍历，以及删除节点时 O(度数) 级联删除关联边。

### 候选事务（candidate transaction）
`Apply` 先在持锁状态下把 `nodes/edges/out/in` 复制为候选副本，按顺序在副本上执行所有操作；任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量超限（`ErrCapacity`）都直接丢弃副本，原状态不变，从而实现整体回滚。全部成功才用副本一次性替换内部状态，并将 `generation` 加一。空批次不改变 `generation`。

### 所有权与并发
- 单把 `sync.RWMutex` 保护全部内部状态：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写互斥、读读并发。
- `Snapshot` 返回的节点/边切片是新分配并排序后的副本（节点按字典序，边按 `(From, To)` 字典序），调用方修改返回值不影响内部状态；图本身不持有调用方传入的切片。
- 校验分两阶段：先做纯结构校验（kind 合法、名称非空且只含 `[a-z0-9-_]`、长度不超限、节点操作不得带 `To`），再读取状态做语义校验。

### 复杂度
- `Apply`：O(N + E) 复制候选状态，加上每个操作 O(1) 均摊（加边含一次环检测 DFS，O(N + E)）；批次末容量检查 O(1)。
- `Reachable`：O(N + E) DFS。
- `Snapshot`：O(N log N + E log E) 排序。
- 空间：O(N + E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
