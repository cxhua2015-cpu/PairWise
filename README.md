# controlgraph088

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

内部状态 `state` 由三张表组成：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From, To}` 为可比较键），O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与删点级联。

## 候选事务（candidate transaction）

`Apply` 先在持锁状态下 `clone()` 出一份候选状态，把所有操作顺序应用到
候选上；任一操作失败（`ErrExists` / `ErrNotFound` / `ErrCycle`）或批次末
容量检查失败（`ErrCapacity`）时直接丢弃候选，原状态不受影响，实现整体
回滚。全部成功才原子地替换 `g.st` 并将 `generation` 加一（空批次不增加）。
批次开始前先对全部操作做纯结构校验（kind 合法、名称合法、无多余字段），
不读取任何状态，因此非法批次永远返回 `ErrInvalidInput`。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable` / `Snapshot` 持读锁。
- `Snapshot` 返回的 `Nodes` / `Edges` 是新分配的切片，节点按字典序、边按
  `(From, To)` 稳定排序；调用方修改返回值不影响内部状态。
- `Reachable` 在读锁内基于当前一致快照做 DFS，不会观察到批次中间态。
- 候选状态由 `Apply` 独占拥有，提交前绝不逃逸，无需额外同步。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数：

- `New`：O(1)。
- `Apply`：克隆 O(N + E)；每个 `AddEdge` 的环检测为一次 DFS，O(N + E)；
  `DeleteNode` 级联 O(N + E)；整体 O(B·(N + E))。
- `Reachable`：O(N + E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
