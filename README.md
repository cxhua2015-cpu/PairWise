# topologygraph218

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `adj map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 做 DFS/BFS。
- 三者冗余存储、在同一临界区内一致更新。

### 候选事务（原子批次）
`Apply` 在写锁内先对全部 op 做纯结构校验（kind 合法、名称字符集/长度、无多余字段），再克隆 `nodes/edges/adj` 为候选状态，顺序应用各 op（存在性、环检测等语义校验在候选上进行）。批次末统一检查最终节点/边容量；任何失败直接丢弃候选，原状态零改动，实现整体回滚。非空成功批次 `generation` 恰好 +1，空批次不变。

### 所有权与并发
- 单把 `sync.RWMutex`：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读操作看到的是同一一致快照。
- `Snapshot` 返回新建切片（节点字典序、边按 `(From,To)` 稳定排序），调用方修改返回值不影响内部状态。
- 环检测与可达性共用 `reaches`（迭代式 DFS），`AddEdge from→to` 当且仅当 `to` 已可达 `from` 时拒绝（含自环）。

### 复杂度
- `Apply`：校验 O(L)，候选克隆 O(V+E)，每 op O(1) 均摊（`DeleteNode` 为 O(E) 扫描关联边，`AddEdge` 环检测 O(V+E)），容量检查 O(1)。
- `Reachable`：O(V+E)。`Snapshot`：O(V log V + E log E)。
- 空间：O(V+E)。

## 验证
`go test ./...`、`go test -race ./...`、`go run ./cmd/demo` 全部通过。
