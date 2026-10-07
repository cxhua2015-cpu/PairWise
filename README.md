# topologygraph368

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 实现说明

**索引**
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[edgeKey]struct{}`：边集合，O(1) 去重/查找。
- `out` / `in map[string]map[string]struct{}`：出边与入边邻接索引，用于环检测 DFS、`Reachable` 遍历，以及 `DeleteNode` 时 O(关联边数) 级联删除。

**候选事务（candidate）**
`Apply` 先对整个批次做纯结构校验（kind 合法、无多余字段、名称符合 `[a-z0-9_-]` 且长度受限），不读取任何状态；随后在写锁内把当前状态深拷贝为候选事务，按序应用全部操作。任一步失败或批次末容量（`MaxNodes`/`MaxEdges`）超限，直接丢弃候选，图状态与 generation 完全不变（整体回滚）；全部成功才一次性提交并使 generation 恰好 +1。空批次成功但 generation 不变。

**所有权与并发**
所有公开方法均可并发调用：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 持读锁，因此 `Reachable` 总是基于某一已提交批次之后的一致快照。`Snapshot` 返回的节点（字典序）与边（先 `From` 后 `To`）为稳定排序的新建切片，与内部状态完全隔离，调用方可自由修改。`Reachable` 只读遍历，不复制图。

**复杂度**（N=节点数，E=边数，K=批次数）
- `Apply`：结构校验 O(K·名称长度)；候选拷贝 O(N+E)；`AddEdge` 环检测最坏 O(N+E)；容量检查 O(1)；提交 O(1)（交换指针）。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
