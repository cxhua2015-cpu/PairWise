# controlgraph178

并发安全的内存型控制面依赖图。见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out` / `in map[string]map[string]struct{}`：出边/入边邻接表，用于可达性 DFS 与删除节点时级联删边。

## 候选事务

`Apply` 分三阶段：

1. **结构校验**：先校验所有 op 的 kind、名称字符集（`[a-z0-9-_]`、非空、≤ `MaxNameBytes`）与字段约束，任何失败返回 `ErrInvalidInput`，不读状态。
2. **候选执行**：克隆 `nodes/edges/out/in` 为候选副本，按序在副本上应用 op；任何语义错误（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本，原状态不变。
3. **容量检查**：仅在批次末比较 `len(nodes)/len(edges)` 与上限，超限返回 `ErrCapacity` 并整体回滚；成功则原子替换内部状态，`generation` 恰好 +1（空批次不变）。

加边时用 DFS 检查 `to` 是否已可达 `from`（含自环），可达则返回 `ErrCycle`。

## 所有权

- 所有公开方法持有 `sync.RWMutex`：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁。
- `Snapshot` 返回新建并排序的切片（节点按字典序、边按 `(From,To)`），与内部状态完全隔离，调用方可自由修改。
- 图内不存储任何调用方提供的切片或映射。

## 复杂度

- `Apply`：结构校验 O(Σ|op|)；候选克隆 O(N+E)；每个 AddEdge 的环检查 O(N+E)；整体 O(N+E+Σop·(N+E))，最坏 O(B·(N+E))。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)。
- 空间：O(N+E)。
