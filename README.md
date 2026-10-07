# topologygraph348

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，
容量仅在批次末检查，失败整体回滚。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 节点集：`map[string]struct{}`，O(1) 存在性判断。
- 边集：`map[Edge]struct{}`，以 `{From, To}` 为键，O(1) 查重与删除。
- 邻接表不持久化：环检测与 `Reachable` 按需从边集构建临时邻接表做迭代式 DFS，
  避免维护双份索引带来的不一致风险；删除节点时直接扫描边集级联删除。

### 候选事务
`Apply` 先做纯结构校验（kind 合法、字段不多不少、名称字符集与字节上限），不读任何状态；
校验通过后克隆节点集与边集作为候选状态，在候选上顺序应用全部操作并做存在性/环检查，
最后统一检查节点与边容量。任一步失败直接丢弃候选，原状态零改动，天然实现整体回滚；
成功则一次性交换内部状态并将 `generation` 加一。空批次不改动 generation。

### 所有权与并发
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Reachable` / `Snapshot` 持读锁，读写互不交错，
  因此 `Reachable` 总是基于某一已提交批次之后的一致快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），
  调用方修改返回值不影响内部状态；内部 map 在提交后不再被旧快照引用。

### 复杂度
设批次数 B、单批操作数 K、节点数 N、边数 E：
- `Apply`：结构校验 O(K·L)（L 为名称长度）；克隆 O(N+E)；
  每个 `AddEdge` 环检测 O(N+E)，`DeleteNode` 级联 O(E)；容量检查 O(1)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
