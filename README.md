# controlgraph163

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。实现见 `controlgraph163/trustgraph.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

**所有权与并发**
- `Graph` 内部状态（节点集合、边集合、generation）由一把 `sync.RWMutex` 保护；`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，所有公开方法可并发调用。
- `Snapshot` 返回的切片是新建的独立副本并做了稳定排序（节点按字典序，边按 `(From, To)`），调用方修改返回值不影响内部状态。
- 图对外不共享任何内部 map/切片，所有权始终留在 `Graph` 内。

**索引**
- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 查重与删除；未额外维护邻接表，遍历边集即得邻接关系，规模受 `MaxEdges` 上限约束。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind 合法、名称字符集/长度、节点操作不得带 `To`），不通过返回 `ErrInvalidInput`，全程不读状态。
- 校验通过后，在写锁内把节点/边 map 复制为候选副本，按顺序在副本上应用所有操作（存在性、环检测等错误立即返回）；仅当全部操作成功且**批次末**容量（`MaxNodes`/`MaxEdges`）检查通过时，才用候选副本原子替换内部状态并将 generation 加一。
- 任一步失败直接丢弃副本，内部状态保持原样，实现整体回滚；空批次不修改状态也不增加 generation。
- 环检测：加边 `from->to` 前在候选边集上检查 `to` 是否可达 `from`（含自环），可达则返回 `ErrCycle`。
- 删除节点时级联删除候选副本中所有与其关联的边。

**复杂度**（N=节点数，E=边数，B=批次操作数）
- `New`：O(1)。
- `Apply`：结构校验 O(B·名称长度)；候选复制 O(N+E)；环检测每次加边 O(E)，整体 O(B·E)；容量检查 O(1)。
- `Reachable`：O(E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
