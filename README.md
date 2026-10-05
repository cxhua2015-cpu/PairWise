# pipelinegraph

并发安全的内存型流水线依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
图状态由三张表组成：`nodes`（节点名集合）、`edges`（`Edge{From,To}` 集合）以及出边邻接表 `out map[string]map[string]struct{}`。邻接表使环检测与 `Reachable` 的 DFS/BFS 只访问实际可达的边，删除节点时也能 O(出度) 清理出边；入边通过一次全量扫描邻接表清理（见复杂度）。

### 候选事务（candidate transaction）
`Apply` 先在无锁环境下对整个批次做纯结构校验（kind 合法、字段齐全、名称合法），不读取任何状态；随后在写锁内把当前状态**深拷贝为候选状态**，按序在候选上应用每个操作（存在性、环检测等校验失败即返回，候选被丢弃，实现整体回滚）。批次末尾才检查节点/边容量上限，超限同样丢弃候选。全部通过后用候选原子替换当前状态，且非空成功批次 `generation` 恰好加一，空批次不变。

### 所有权与并发
所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁（`sync.RWMutex`），因此 `Reachable` 总是基于某一一致快照求值。`Snapshot` 返回的节点（字典序）与边（先 `From` 后 `To`）为稳定排序的新切片，与内部状态完全隔离，调用方可自由修改。环检测在加边前以 `To → From` 可达性判断实现，自环也因此被拒绝。

### 复杂度
设 N 为节点数、E 为边数、B 为批次内操作数：

- `Apply`：候选拷贝 O(N+E)；每个 `AddEdge` 环检测 O(N+E)；`DeleteNode` 清理入边 O(N+E)；整体 O(B·(N+E))。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 使用

```go
g, _ := pipelinegraph.New(pipelinegraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(pipelinegraph.Batch{Ops: []pipelinegraph.Op{
    {Kind: pipelinegraph.AddNode, From: "a"},
    {Kind: pipelinegraph.AddNode, From: "b"},
    {Kind: pipelinegraph.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot()
```

运行示例：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。
