# topologygraph428

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为五个文件：

- `trustgraph.go` — 核心事务引擎：`Graph` 状态、`Apply`/`Reachable`/`Snapshot`、候选事务回放。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的 `Stats` 摘要。
- `clone.go` — 保留逻辑时钟（generation）且所有权完全隔离的深拷贝。
- `preview.go` — 在一次线性化快照上复用完整事务语义做预演，返回候选 `Result`/`Snapshot`/`Stats`，不改变原对象。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From,To}` 为可比较键），O(1) 查重与删除。
- 并发：单把 `sync.RWMutex`；写事务（`Apply`）独占，读路径（`Reachable`/`Snapshot`/`Stats`/`Clone`/`Preview` 的快照获取）共享读锁。

## 候选事务（candidate transaction）

`Apply` 先做完整结构校验（不读状态），再在锁内把节点/边复制到候选 map 上按序回放操作；任一语义错误（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量超限（`ErrCapacity`）都会丢弃候选，实现整体回滚。只有全部成功才把候选 map 一次性换入并令 generation 恰好 +1；空批次不增加 generation。`Preview` 通过 `Clone` 得到候选对象后直接复用 `Apply`，因此错误及优先级与同一状态上的真实提交完全一致。

## 所有权

- `Snapshot`/`Stats` 返回的切片与标量均为新建副本，调用方修改不会影响内部状态。
- `Clone` 深拷贝全部 map 与逻辑时钟，克隆体与原对象互不影响。
- `Preview` 的所有返回值来自候选对象，原对象的状态、generation 与逻辑时间保持不变；失败时全部返回零值。

## 复杂度

设 N 为节点数、E 为边数、B 为批次数。

- `Apply`：结构校验 O(B)；回放中 `AddEdge` 的环检测为一次 BFS，O(N+E)；删除节点需扫描边表 O(E)；候选复制 O(N+E)；整体 O(B·(N+E))。
- `Reachable`：BFS，O(N+E)。
- `Snapshot`：排序节点与边，O(N log N + E log E)，排序稳定且确定（节点字典序；边按 From 再 To）。
- `Stats`：O(1)。`Clone`：O(N+E)。`Preview`：等价于一次 `Clone` + 一次 `Apply`。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
