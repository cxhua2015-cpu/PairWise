# controlgraph163

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 使用

```go
g, _ := controlgraph163.New(controlgraph163.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
res, _ := g.Apply(controlgraph163.Batch{Ops: []controlgraph163.Op{
    {Kind: controlgraph163.AddNode, From: "a"},
    {Kind: controlgraph163.AddNode, From: "b"},
    {Kind: controlgraph163.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot() // 节点与边均按字典序稳定排序
```

## 设计说明

**索引**：图状态由四份索引组成——`nodes`（节点集合）、`edges`（边集合，键为 `{from,to}`）、`out`（正向邻接表）、`in`（反向邻接表）。`out` 支撑可达性 DFS 与环检测，`in` 支撑删除节点时 O(度数) 级联清理关联边，两者与 `edges` 始终保持一致。

**候选事务**：`Apply` 先对整个批次做纯结构校验（kind 合法、无多余字段、名称字符与字节长度合法），不读取任何状态；随后在写锁内把当前状态完整克隆为候选副本，按序在副本上执行全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接丢弃副本，原状态与 generation 完全不变；全部成功才原子替换状态指针并将 generation 加一。空批次成功但不推进 generation。

**所有权与并发**：`Graph` 内部用 `sync.RWMutex` 保护，`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此读者永远看到一份完整一致的快照。`Snapshot` 返回的切片是新分配的拷贝，调用方修改不会影响内部状态；状态替换后旧副本不可变地被遗留读者引用，由 GC 回收，无共享可变内存。

**复杂度**（V 节点数、E 边数、B 批次操作数）：
- `Apply`：克隆 O(V+E)，逐操作 O(1)（加边另含一次环检测 DFS，O(V+E)），容量检查 O(1)。
- `Reachable`：一次 DFS，O(V+E)。
- `Snapshot`：O(V+E) 收集 + O(V log V + E log E) 排序。
- 空间：O(V+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
