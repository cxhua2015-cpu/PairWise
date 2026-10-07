# topologygraph388

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 节点集：`map[string]struct{}`，O(1) 存在性判断。
- 边集：`map[Edge]struct{}`，`Edge{From, To}` 为可比较结构体键，O(1) 查重/删除。
- 未维护邻接表；DFS/BFS 与级联删除直接扫描边集，换取实现简单与状态单一（无冗余索引需保持一致）。

### 候选事务（原子批次）
`Apply` 先做**整批结构校验**（kind 合法、字段恰好、名称合法），不读取任何状态；随后在写锁内把节点/边两个 map **浅拷贝为候选副本**，按顺序在副本上执行全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或**批次末**容量检查（`ErrCapacity`）失败时直接丢弃副本，原状态零改动，实现整体回滚。全部成功才用副本替换正式状态，并将 `generation` 恰好加一；空批次不校验通过也不增加 generation。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 持 `sync.RWMutex` 读锁，因此 `Reachable` 总是基于某一时刻的一致快照。
- 内部 map 只在锁内访问；`Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态，所有权完全移交。

### 复杂度
设 N=节点数，E=边数，B=批次操作数：
- 结构校验：O(B · 名称长度)。
- 候选副本：O(N + E)。
- AddNode/DeleteEdge/AddEdge 查重：O(1)；DeleteNode 级联删边：O(E)。
- AddEdge 环检测（从 `To` 反向可达 `From`，含自环）：O(E) BFS。
- 整批：O(B · E)；`Reachable`：O(E)；`Snapshot`：O(N log N + E log E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
