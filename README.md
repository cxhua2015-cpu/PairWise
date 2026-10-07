# topologygraph398

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点
级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `adj map[string]map[string]struct{}`：出边邻接表，供 `Reachable` 的 BFS
  与加边前的环检测（反向可达性）使用；删边时同步维护，空出边集合即回收。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：先对整个批次做纯校验（kind 合法、节点操作不带 `To`、
   名称非空、仅 `[a-z0-9-_]` 且不超过 `MaxNameBytes`），不读取任何状态；
   任何失败返回 `ErrInvalidInput`。
2. **候选执行**：克隆当前状态为候选副本，在其上顺序应用全部操作
   （存在性、环检测、级联删除），最后才检查 `MaxNodes` / `MaxEdges`。
   任一步失败直接丢弃候选，原状态零改动；成功则原子替换并
   `generation++`。空批次或失败批次不改变 generation。

## 所有权

- 所有公开方法通过一把 `sync.RWMutex` 保护：`Apply` 持写锁，
  `Reachable` / `Snapshot` 持读锁，可并发调用。
- `Snapshot` 返回的切片与 `Result` 均为新建副本，调用方修改返回值
  不影响内部状态；内部也绝不保留调用方传入的切片。

## 复杂度

设 N 为节点数、E 为边数、K 为批次内操作数：

- `New`：O(1)。
- `Apply`：克隆 O(N+E)；每个 `AddEdge` 环检测为一次 BFS，O(N+E)；
  `DeleteNode` 级联删除 O(E)；整体 O(N+E+K·(N+E))。
- `Reachable`：BFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)，节点按字典序、
  边按 `(From, To)` 字典序稳定输出。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
