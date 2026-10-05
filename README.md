# rolloutgraph

并发安全的内存型发布依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
`Graph` 内部状态 `state` 维护四份冗余索引，全部随每次变更同步更新：
- `nodes map[string]struct{}`：节点存在性 O(1) 判定；
- `edges map[Edge]struct{}`：边存在性 O(1) 判定；
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与环检测；
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 级联删边只访问关联边而非全图。

### 候选事务（candidate transaction）
`Apply` 分两阶段：
1. **结构校验**：在不读取任何状态的前提下校验全部 op（kind 合法、名称字符集/长度、节点 op 不得携带 `To`）。任一失败返回 `ErrInvalidInput`，状态零接触。
2. **候选提交**：在写锁内将当前 `state` 深拷贝为候选，按序应用全部 op（存在性检查、加边前以 `reachable(to, from)` 做环检测），最后才检查节点/边容量。任一步失败直接丢弃候选，原状态未被触碰，天然实现整体回滚；全部成功则原子地换入候选并将 `generation` 加一。空批次不修改状态也不增加 generation。

### 所有权与并发
- 所有公开方法经 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 总是基于某一个一致的已提交状态求值。
- `state` 一旦换入即不可变（写路径只改候选副本），读路径无需防御性拷贝。
- `Snapshot` 返回新建切片，节点按字典序、边按 `(From, To)` 稳定排序；调用方修改返回值不影响内部状态。

### 复杂度
设批次含 B 个 op，图有 N 个节点、E 条边：
- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选深拷贝 O(N+E)；每个 AddEdge 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。整体 O(N + E + B·(N+E))，空间 O(N+E)。
- `DeleteNode`：O(关联边数)，而非 O(E)。
- `Reachable`：O(N+E) DFS。
- `Snapshot`：O(N log N + E log E) 排序，O(N+E) 额外空间。

## 验证
```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
