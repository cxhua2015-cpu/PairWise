# topologygraph383

并发安全的内存型有向“控制拓扑图 383”，仅依赖 Go 标准库（Go 1.22+）。
原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量只在批次末检查，失败整体回滚。

## 索引

内部状态 `state` 持有四类索引，全部随候选事务一起克隆、整体替换：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除定位。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。
- `in  map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 级联删除为 O(deg)。

## 候选事务（candidate transaction）

`Apply` 分两阶段：先对整批做纯结构校验（kind 合法、必填字段、无多余字段、名称
字符集与长度），不读取任何状态；校验通过后克隆当前状态得到候选副本，在副本上
按序应用全部操作，最后仅在批次末检查节点/边容量。任一失败直接丢弃副本，原状态
与 generation 均不变，实现整体回滚；成功则原子替换内部状态指针并将 generation
加一（空批次不加）。`Reachable` 与 `Snapshot` 在锁内读取当前状态，因此总是看到
某个批次完成后的一致快照。

## 所有权

- 所有公开方法通过单一 `sync.Mutex` 串行化，支持并发调用。
- `Snapshot` 返回的切片是新分配的独立副本，调用方可自由修改，不影响内部状态。
- `Batch`/`Op` 等入参只读，实现不会保留或回写调用方的切片。
- `Graph` 不可复制；请通过 `New` 返回的指针共享使用。

## 复杂度

记 N 为节点数、E 为边数、B 为批次操作数、d 为单点度数：

- `Apply`：克隆 O(N+E)，逐操作 O(1)（`DeleteNode` 为 O(d)），`AddEdge` 环检测
  DFS 为 O(N+E)，容量检查 O(1)；总计 O(N+E+B·(N+E))，实际环检测仅发生在加边操作上。
- `Reachable`：O(N+E) DFS。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 排序；节点按字典序、边按
  (From, To) 字典序稳定输出。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
