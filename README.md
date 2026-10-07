# topologygraph353

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

**索引**
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS/BFS。`DeleteNode` 通过遍历 `edges` 同步清理两个索引，保持一致。

**候选事务（candidate transaction）**
`Apply` 分两阶段：
1. 结构校验：在不持有写锁前检查全部 op 的 kind、名称字符集（非空 ASCII 小写字母/数字/`-`/`_`，长度 ≤ `MaxNameBytes`）与多余字段，失败返回 `ErrInvalidInput`。
2. 在写锁内把 `nodes`/`edges`/`out` 克隆为候选副本，按序应用所有 op（存在性、缺失、环检测），最后才检查节点/边容量。任一步失败直接丢弃候选副本，原状态零改动，实现整体回滚；全部成功才一次性换入并令 `generation++`。空批次不修改状态也不增加 generation。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 持 `sync.RWMutex` 读锁，读写互斥、读读并行。
- `Reachable` 在读锁内对当前一致快照做遍历，不会观察到批次中间态。
- `Snapshot` 返回新分配的切片（节点按字典序、边按 `(From,To)` 稳定排序），调用方修改返回值不影响内部状态；内部 map 永不暴露。

**复杂度**（N=节点数，E=边数，B=批次内 op 数）
- `Apply`：结构校验 O(B·名称长度)；候选克隆 O(N+E)；每个 AddEdge 环检测 O(N+E)；DeleteNode 级联 O(E)；容量检查 O(1)。总计 O(N + E + B·(N+E))。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
