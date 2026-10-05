# dependencygraph

并发安全的内存型有向组件依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。`DeleteNode` 时遍历邻接表清理关联边。

### 候选事务（candidate transaction）
`Apply` 先做整批结构校验（kind、名称字符集与长度、多余字段），不读取任何状态；随后在写锁内把 `nodes/edges/out` 克隆为候选副本，按序在副本上执行全部操作。任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本，原状态零改动。全部成功后仅在批次末检查节点/边容量，超限返回 `ErrCapacity` 并整体回滚；否则用副本原子替换内部状态。非空成功批次 `generation` 恰好加一，空批次与失败批次不变。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 持读锁，因此 `Reachable` 总是基于当前一致快照。
- 返回的 `Snapshot` 切片为新分配并排序（节点字典序、边按 `(From, To)` 字典序），调用方修改返回值不影响内部状态；图提交后不再持有候选副本以外的共享引用。
- 名称仅允许非空 ASCII 小写字母、数字、`-`、`_`，且不超过 `Options.MaxNameBytes`。

### 复杂度
设批次含 k 个操作，V 个节点、E 条边：
- `Apply`：克隆 O(V+E)；每条 `AddEdge` 环检测 DFS O(V+E)；容量检查 O(1)；整体 O(V+E) 外加 k 次操作。
- `Reachable`：DFS O(V+E)。
- `Snapshot`：O(V log V + E log E)（排序）。
- 空间：O(V+E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
