# topologygraph298

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删节点级联删边，容量只在批次末检查，失败整体回滚。

## Multi-file architecture

实现刻意拆分为四个联动组件，共享同一套结构语义：

- `trustgraph.go` — 核心事务引擎：`Graph`、`New`、`Apply`、`Reachable`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 只做结构校验（kind、名称文法、
  自环、多余字段），不读取也不修改图状态；`Apply` 复用同一份 `validateOp`/`validateName`。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回 generation、节点数、边数。
- `clone.go` — 所有权安全的深拷贝：`Clone` 复制全部索引与逻辑时钟（generation），
  与原图零共享。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 可比较，直接作键），O(1) 查重/删除。
- 未维护邻接表；遍历（BFS、级联删除）直接扫描边集，保持索引最少、不变量最简单。

## 候选事务（candidate transaction）

`Apply` 先调用 `ValidateBatch` 做完整结构预检（此时不读状态），然后在写锁内把
当前节点/边索引复制为候选状态，顺序应用全部 op；任一 op 失败或批次末
`len(nodes) > MaxNodes` / `len(edges) > MaxEdges` 即丢弃候选，原状态不变。
全部成功才一次性交换索引并将 `generation` 递增一次；空批次不递增。

## 所有权与并发

- 单把 `sync.RWMutex` 保护全部状态：写者互斥，读者（`Reachable`/`Snapshot`/`Stats`/`Clone`）
  在读锁下看到一致的已提交快照，满足线性一致。
- 所有返回的切片（`Snapshot.Nodes`/`Edges`）都是新分配并按字典序稳定排序的副本，
  调用方修改不会污染内部状态；`Clone` 的 map 全部新建，两个图互不影响。

## 复杂度

设 N=节点数，E=边数，B=批次内 op 数：

- `Apply`：结构预检 O(B·L)（L 为名称长度）；候选复制 O(N+E)；
  AddNode/DeleteEdge O(1)，DeleteNode O(E)（级联扫描），AddEdge O(E)（BFS 判环）；
  整体 O(N + E + B·E)。
- `Reachable`：O(E) BFS。`Snapshot`：O(N log N + E log E) 排序。
- `Stats`：O(1)。`Clone`：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
