# topologygraph318

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点
级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

`Graph` 持有四组索引，全部随每次变更同步维护：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边存在性，O(1) 判重。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与环检测。
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 的级联删边
  只触及关联边而非全图扫描。

## 候选事务（回滚）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态之前校验全部 op 的 kind、名称字符集
   （`[a-z0-9-_]`、非空、不超过 `MaxNameBytes`）及多余字段，失败返回
   `ErrInvalidInput`，不加写锁以外的状态变更。
2. **候选执行**：在写锁内就地顺序应用，同时记录撤销日志（新增节点、删除节点
   及其被级联删除的边、新增边、删除边）。任一 op 失败
   （`ErrExists` / `ErrNotFound` / `ErrCycle`）或批次末容量检查失败
   （`ErrCapacity`）时，按逆序回放撤销日志，图恢复到批次前状态，generation
   不变。非空成功批次 generation 恰好 +1；空批次成功且 generation 不变。

容量只在批次末检查，因此批次中途允许暂时超限（例如先删后增的替换操作）。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable` / `Snapshot` 取读锁，
  读写互斥、读读并行。`Reachable` 在读锁内对当前一致快照做 DFS。
- `Snapshot` 返回的 `Nodes`（按字典序）与 `Edges`（按 `(From, To)` 字典序）
  为新分配的切片，调用方修改不影响内部状态；批次失败不暴露中间态。
- 名称与边键为值类型字符串/结构体，内部不保留调用方切片的引用。

## 复杂度

设 N 为节点数、E 为边数、K 为批次内 op 数：

- `New`：O(1)。
- `Apply`：结构校验 O(K·L)（L 为名称长度）；每个 `AddEdge` 的环检测为一次
  DFS，O(N + E)；`DeleteNode` 级联 O(关联边数)；回滚 O(K + 级联边数)。
  整体 O(K·(N + E)) 上界。
- `Reachable`：O(N + E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
