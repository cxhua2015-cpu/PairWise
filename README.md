# controlgraph158

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- `adj map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS，以及 `DeleteNode` 时正向清理出边。

## 候选事务（原子批次）

`Apply` 在写锁内先对整批做纯结构校验（kind 合法、字段不多不少、名称合法），不读取任何状态；随后把 `nodes/edges/adj` 克隆为候选状态，按序在候选上应用每个操作（存在性、环检测等失败即整批放弃），最后才检查节点/边容量。全部通过则一次性换入候选并将 `generation` 加一；任何失败都直接丢弃候选，原状态零改动，实现整体回滚。空批次成功但不改变 generation。

## 所有权

- 所有公开方法（`Apply`/`Reachable`/`Snapshot`）均可并发调用：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读写互斥且读读并行。
- `Snapshot` 返回的切片均为新建并排序后的副本（节点按字典序、边按 `(From,To)` 稳定排序），调用方修改返回值不影响内部状态；内部 map 也绝不逃逸到返回值中。
- 环检测将自环视为环；`Reachable(x, x)` 对存在的节点返回 `true`。

## 复杂度

设 V 为节点数、E 为边数、B 为批次操作数：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选克隆 O(V+E)；每个 `AddEdge` 的环检测为一次 DFS，O(V+E)；`DeleteNode` 清理关联边 O(V+E)；整体 O(B·(V+E))，容量检查 O(1)。
- `Reachable`：一次 DFS，O(V+E)。
- `Snapshot`：收集 O(V+E)，排序 O(V·logV + E·logE)。
- 空间：O(V+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
