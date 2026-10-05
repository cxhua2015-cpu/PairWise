# controlgraph103

并发安全的内存型有向“控制面依赖图”。原子批次支持 `AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删节点级联删除关联边，容量只在批次末检查，失败整体回滚。仅依赖标准库，Go 1.22+。

## 索引结构

- `nodes map[string]struct{}`：节点集合。
- `edges map[Edge]struct{}`：权威边集合，独立于遍历顺序保证去重判定。
- `out` / `in map[string]map[string]struct{}`：出边 / 入边邻接索引。`out` 支撑环检测与 `Reachable` 的 DFS；`in` 使 `DeleteNode` 级联删除为 O(关联边数) 而非全图扫描。

## 候选事务（candidate transaction）

`Apply` 先在**不读任何状态**的情况下对整个批次做结构校验（kind、名称字符集与字节上限、多余字段），失败返回 `ErrInvalidInput`。随后在写锁内把四张索引**深拷贝**为候选事务，所有状态相关检查（`ErrExists` / `ErrNotFound` / `ErrCycle`）与变更都作用于副本；任何一步失败直接丢弃副本即完成回滚，已提交状态零改动。容量（`MaxNodes` / `MaxEdges`）只对批次末的最终状态检查 `ErrCapacity`，批次内临时超限合法。全部通过后一次性换入副本；非空成功批次 `generation` 恰好 +1，空批次不变。

## 并发与所有权

- 单个 `sync.RWMutex` 串行化全部读写：`Apply` 持写锁，`Reachable` / `Snapshot` 持读锁，读到的是同一 generation 的一致快照。
- `Snapshot` 返回新建切片（节点字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态；`Options` 在 `New` 后只读，校验可无锁进行。

## 复杂度

设批次大小 B、节点数 N、边数 E：

- `Apply`：结构校验 O(B·名称长度)；候选拷贝 O(N+E)；每条 `AddEdge` 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。
- `DeleteNode`：O(该节点关联边数)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
