# controlgraph083

并发安全的内存型“控制面依赖图”，Go 1.22+，仅标准库。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量仅在批次末检查，失败整体回滚。

## 索引

`Graph` 内部维护四类索引，全部随批次原子替换：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。
- `in  map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(关联边数) 级联删除。

## 候选事务（candidate）

`Apply` 先对整个批次做纯结构校验（kind、名称字符集与长度、多余字段），
不读任何状态；然后在写锁内把当前状态浅拷贝为 `candidate`（节点/边集合 +
两个邻接表），在候选上顺序应用所有操作。任一操作失败（`ErrExists` /
`ErrNotFound` / `ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接
丢弃候选，原状态零改动——天然回滚。全部成功才用候选整体替换内部状态，
并将 `generation` 恰好加一；空批次成功但 generation 不变。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取写锁，`Reachable` / `Snapshot` 取读锁，
  读操作看到的是同一把锁保护下的一致快照。
- `Snapshot` 返回的 `Nodes`（按字典序）与 `Edges`（按 `(From, To)` 稳定排序）
  是新分配的切片，调用方可自由修改，不影响内部状态，反之亦然。
- `Reachable` 在持锁期间于当前邻接表上 DFS，结果对应某一一致时刻。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数、D 为被删节点的关联边数：

- `Apply`：结构校验 O(B·名称长度)；候选拷贝 O(N+E)；每操作均摊 O(1)，
  其中 `AddEdge` 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。
- `DeleteNode`：O(D)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 使用

```go
g, _ := controlgraph083.New(controlgraph083.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(controlgraph083.Batch{Ops: []controlgraph083.Op{
    {Kind: controlgraph083.AddNode, From: "a"},
    {Kind: controlgraph083.AddNode, From: "b"},
    {Kind: controlgraph083.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
s := g.Snapshot()
```

运行示例：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。
