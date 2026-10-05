# controlgraph183

并发安全的内存型“控制面依赖图 183”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 判重/删除。
- `adj map[string]map[string]struct{}`：正向邻接表，供环检测与 `Reachable` 做 DFS/BFS。`edges` 与 `adj` 始终同步维护。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限、多余字段），不读取任何图状态。
- 校验通过后在写锁内克隆 `nodes`/`edges`/`adj` 为候选状态，逐条施加操作；任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃候选，原状态零改动。
- 节点/边容量只在批次末对最终候选状态检查，超限返回 `ErrCapacity` 并整体回滚；因此批次内可以“先删后加”临时超容。
- 全部成功才用候选状态原子替换提交，`generation` 恰好加一；空批次成功但不改变 generation。

**所有权与并发**
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，因此 `Reachable` 看到的始终是某个已提交批次之后的一致快照。
- `Snapshot` 返回的 `Nodes`/`Edges` 为新分配切片，节点按字典序、边按 `(From, To)` 稳定排序；调用方修改返回值不影响内部状态，反之亦然。

**复杂度**（N=节点数，E=边数，B=批次操作数）
- `New`：O(1)。
- `Apply`：结构校验 O(B)；克隆 O(N+E)；每条操作均摊 O(1)，其中 `AddEdge` 的环检测为一次 DFS，O(N+E)；`DeleteNode` 级联删除 O(N+E)；批次末容量检查 O(1)。整体 O(N+E+B·(N+E)) 上界。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N log N + E log E)（拷贝加排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
