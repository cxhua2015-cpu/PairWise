# topologygraph353

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 节点集：`map[string]struct{}`，O(1) 存在性判断。
- 边集：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 查重与删除。
- 未维护邻接表；遍历（DFS、删节点级联、Snapshot）直接扫描边集，以内存与实现简单性换取 O(E) 遍历。

### 候选事务（candidate transaction）
`Apply` 先在持锁状态下对整个批次做**纯结构校验**（kind 合法、字段不多余、名称字符集与字节上限），不读取图状态；随后把节点/边两个 map 复制为候选副本，所有操作按序作用于副本。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接丢弃副本，实现整体回滚；全部成功才用副本替换内部状态并将 `generation` 递增一次。空批次不修改状态也不递增 generation。

### 所有权与并发
- `Graph` 内全部状态由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 总是基于某一一致快照。
- 返回值隔离：`Snapshot` 新建切片并拷贝数据，`Result`/`Snapshot` 不含任何内部引用，调用方修改返回值不影响图。
- 环检测与 `Reachable` 使用迭代式 DFS（显式栈 + visited 集），无递归深度风险。

### 复杂度
设 N=节点数，E=边数，B=批次操作数：
- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选复制 O(N+E)；AddNode/DeleteEdge/AddEdge 均摊 O(1)，AddEdge 的环检测 O(E)，DeleteNode 级联 O(E)；末尾容量检查 O(1)。
- `Reachable`：O(E)。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 稳定排序（节点按字典序，边按 `(From, To)` 字典序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
