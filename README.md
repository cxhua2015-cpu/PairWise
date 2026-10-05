# replicationgraph

并发安全的内存型有向复制拓扑图（Go 1.22+，仅标准库）。见 `SPEC.md`。

## 设计

### 索引

`Graph` 持有三份冗余索引，均由同一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边集合，O(1) 查询重复边 / 删除边。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与环检测；删除节点时反向扫描该表清除入边。

### 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前校验全部 op 的 kind、名称字符集（非空 ASCII 小写字母/数字/`-`/`_`，长度 ≤ `MaxNameBytes`）与多余字段，失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把当前状态浅拷贝为 candidate，按序应用全部 op（存在性、环、重复边检查都在 candidate 上做）。任何一步失败直接丢弃 candidate，原图零改动——天然回滚。节点/边容量上限只在批次末对 candidate 检查，超限返回 `ErrCapacity` 并整体回滚。成功后用 candidate 整体替换内部状态，`generation` 恰好 +1；空批次与失败批次不增加。

### 所有权

- `Snapshot` 返回的 `Nodes`/`Edges` 是新分配的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态。
- `Reachable` 在 `RLock` 下对当前一致快照做 DFS，不复制数据、不暴露内部引用。
- candidate 只在 `Apply` 的写锁临界区内存活，绝不逃逸给调用方。

### 复杂度

设 N=节点数，E=边数，B=批次数。

- `Apply`：结构校验 O(B·名称长度)；candidate 拷贝 O(N+E)；每个 op O(1) 均摊，AddEdge 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E)，读锁，可与其他只读调用并发。
- `Snapshot`：O(N+E) 拷贝 + O(N log N + E log E) 排序，读锁。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
