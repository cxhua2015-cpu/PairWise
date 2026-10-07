# topologygraph303

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点
级联删除关联边，容量只在批次末检查，失败整体回滚。详见 `SPEC.md`。

## 设计

### 索引

`Graph` 持有四份相互一致的索引：

- `nodes map[string]struct{}`：节点存在性集合。
- `edges map[Edge]struct{}`：边存在性集合（`Edge{From, To}` 可比较，直接作键）。
- `adj map[string]map[string]struct{}`：正向邻接表，用于环检测与 `Reachable` 的 DFS。
- `reverse map[string]map[string]struct{}`：反向邻接表，使 `DeleteNode` 能 O(入度)
  找到并级联删除入边，无需全图扫描。

### 候选事务（staging）

`Apply` 分三个阶段：

1. **结构校验**：对整个批次做纯结构检查（kind 合法、字段齐全、名称字符集与
   字节上限），不读取任何图状态；任一 op 非法即返回 `ErrInvalidInput`。
2. **暂存执行**：克隆四份索引得到候选状态，在候选上按序执行所有 op
   （存在性、环检测均基于候选状态，因此同批次内先加后删等序列语义正确）。
   任何失败直接丢弃候选，原状态零改动。
3. **容量与提交**：仅在批次末比较 `MaxNodes` / `MaxEdges`，超限返回
   `ErrCapacity` 并整体回滚；否则原子地换入候选索引，`generation` 恰好 +1。
   空批次成功但不改变 generation。

### 所有权与并发

- 所有公开方法并发安全：`Apply` 持写锁，`Reachable` / `Snapshot` 持读锁，
  因此 `Reachable` 总是基于某一时刻的一致快照做遍历。
- 所有权边界清晰：图内部状态绝不外泄。`Snapshot` 返回新建切片（节点按字典序、
  边按 `(From, To)` 稳定排序），调用方修改返回值不影响图；`Apply` 只读取入参，
  不保留其引用。

### 复杂度

设 V 为节点数、E 为边数、批次长 B：

- `Apply`：结构校验 O(B)；暂存克隆 O(V + E)；每个 op O(1) 均摊，`AddEdge`
  的环检测为一次 DFS，O(V + E)；容量检查 O(1)。整体 O(V + E + B)。
- `Reachable`：一次 DFS，O(V + E)。
- `Snapshot`：O(V + E) 收集 + O(V log V + E log E) 排序。
- 空间：O(V + E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
