# topologygraph313

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引结构

图状态 `state` 由四张表组成：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除定位。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性遍历与环检测。
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(关联边数) 级联删除，无需全表扫描。

## 候选事务（candidate transaction）

`Apply` 先在**不读任何状态**的情况下对整批 op 做结构校验（kind 合法、无多余字段、名称字符与长度合规），失败返回 `ErrInvalidInput`。随后在写锁内把当前 `state` 深拷贝为候选状态，按序在候选上执行全部操作；任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃候选，原状态不受影响。节点/边容量上限只在批次末尾对候选状态检查，超限返回 `ErrCapacity` 并整体回滚——因此批次中途允许瞬时超限。全部通过后原子换入候选状态，`generation` 恰好加一；空批次成功但不改变 generation。

## 所有权与并发

- 所有公开方法可并发调用：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁（`sync.RWMutex`）。
- 状态一旦换入即不可变，只在候选副本上修改，读路径看到的是一致的快照。
- `Snapshot` 返回的切片均为新建拷贝，节点按字典序、边按 `(From, To)` 稳定排序；调用方修改返回值不影响内部状态。

## 复杂度

设 N 为节点数、E 为边数、B 为批次 op 数：

- `Apply`：结构校验 O(B·名称长度)；候选克隆 O(N+E)；每个 AddEdge 的环检测为一次 BFS，O(N+E)；容量检查 O(1)。
- `Reachable`：一次 BFS，O(N+E)，在读锁内基于当前一致快照。
- `Snapshot`：O(N log N + E log E) 排序，切片为新分配内存。
- `DeleteNode`：O(该节点关联边数)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
