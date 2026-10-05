# controlgraph093

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1) 判定。
- `edges map[Edge]struct{}`：边存在性 O(1) 判定（`Edge{From,To}` 为可比较键）。
- `adj map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 的 DFS 使用；边删除时同步维护，空邻接表即时回收。

### 候选事务（原子批次）
`Apply` 先在**不读取图状态**的情况下对全部 Op 做结构校验（kind 合法、名称字符集与字节上限、节点操作不得携带 `To`），失败返回 `ErrInvalidInput`。随后持有写锁，将操作逐个应用到图上并记录**逆操作日志**（undo log）；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（节点数/边数超过 `Options` 上限）失败（`ErrCapacity`）时，按逆序回放 undo 日志整体回滚，图状态与 generation 保持不变。容量只在批次末检查，因此“先删后增”的替换型批次不受中间态超限影响。非空成功批次 generation 恰好 +1；空批次成功但 generation 不变。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁（`sync.RWMutex`），读写互斥、读读并行。
- `Reachable` 在读锁内基于当前一致快照做 DFS，不会观察到批次的中间状态。
- `Snapshot` 在锁内拷贝节点与边并按 `(From, To)` 字典序稳定排序后返回；返回的切片为全新分配的副本，调用方修改不影响内部状态，后续批次也不会改写已返回的切片。
- `Graph` 不可复制；通过 `New` 返回的指针共享使用。

### 复杂度
设批次含 k 个操作，图含 N 个节点、E 条边：
- `Apply`：结构校验 O(k·L)（L 为名称长度）；每个 AddEdge 的环检测为一次 DFS，O(N+E)；DeleteNode 级联删边 O(E)；容量检查 O(1)。整体 O(k·(N+E))。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。
- 空间：O(N+E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
