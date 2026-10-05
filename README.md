# workflowgraph

并发安全的内存型工作流依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除。
- `out map[string]map[string]struct{}`：出边邻接索引，供环检测与 `Reachable` 做图遍历；删除节点时扫描边集合并联级清理。

### 候选事务（candidate transaction）
`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限、多余字段），不读取任何状态；随后在写锁内把 `nodes`/`edges`/`out` 克隆为候选副本，在副本上顺序应用全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接丢弃副本，实现整体回滚；全部成功才一次性替换内部状态并将 `generation` 加一。空批次不修改状态、generation 不变。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写互斥且读读可并行。
- `Reachable` 在读锁内基于当前一致快照遍历，不会看到批次中间态。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方对返回值的修改不影响内部状态；`Result`/`Snapshot` 均为值语义，无内部指针泄漏。

### 复杂度
设批次操作数为 k，节点数 n，边数 m。
- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选克隆 O(n+m)；单操作均摊 O(1)，其中 `AddEdge` 的环检测为一次 DFS，O(n+m)，`DeleteNode` 级联扫描 O(m)；容量检查 O(1)。
- `Reachable`：O(n+m)。
- `Snapshot`：O(n log n + m log m)（排序）。
- 空间：O(n+m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
