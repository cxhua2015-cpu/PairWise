# controlgraph168

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

- 节点集：`map[string]struct{}`，O(1) 存在性判断。
- 边集：`map[Edge]struct{}`，`Edge{From, To}` 为可比较键，O(1) 查重与删除。
- 未维护持久化邻接表；环检测与 `Reachable` 在边集上即时 DFS。

## 候选事务（candidate transaction）

`Apply` 先做纯结构校验（kind 合法、名称合法、无多余字段），不读图状态；
随后在写锁内把 `nodes`/`edges` 克隆为候选副本，按序在副本上应用全部 op
（存在性、缺失、环检测均针对副本）。任一步失败或批次末容量
（`MaxNodes`/`MaxEdges`）超限即丢弃副本，原状态零改动、generation 不变；
全部成功才原子换入并令 generation 加一。空批次成功但 generation 不变。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，
  读操作看到的是同一一致快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），
  调用方修改返回值不影响内部状态；`Reachable` 不暴露内部引用。
- 名称仅允许非空 ASCII 小写字母、数字、`-`、`_`，且不超过 `MaxNameBytes`。

## 复杂度

设 N=节点数，E=边数，K=批次内 op 数。

- `Apply`：克隆 O(N+E)；每个 `AddEdge` 的环检测为 O(E) DFS；
  `DeleteNode` 级联扫描 O(E)；整体 O(N + E + K·E)。
- `Reachable`：O(E)。
- `Snapshot`：O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
