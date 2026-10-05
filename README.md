# controlgraph198

并发安全的内存型“控制面依赖图”，仅依赖 Go 标准库（Go 1.22+）。公开 API 与错误值见 `controlgraph198/trustgraph.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点存在性 O(1) 判定。
- `edges map[Edge]struct{}`：边存在性 O(1) 判定（用于 `ErrExists`/`ErrNotFound`）。
- `adj map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 做 DFS。
- 三者同源同事务更新，删除节点时级联删除关联边并清理空邻接桶。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集/字节上限、节点操作不得带 `To`），不通过返回 `ErrInvalidInput`，完全不读状态。
- 校验通过后克隆 `nodes`/`edges`/`adj` 为候选状态，在候选上顺序执行操作；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃候选。
- 节点/边容量只在批次末对候选状态检查，超限返回 `ErrCapacity` 并整体回滚。
- 全部成功才一次性换入候选状态并将 `generation` 加一；空批次成功但不增加 generation，失败批次也不增加。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁，读写互斥且读读可并行。
- `Reachable` 在读锁内基于当前一致快照做 DFS，不会观察到批次中间态。
- `Snapshot` 在锁内拷贝节点与边并按字典序稳定排序（节点升序；边按 `(From, To)` 升序），返回的切片为新建内存，调用方修改不影响内部状态。

**复杂度**（N=节点数，E=边数，B=批次操作数）
- `Apply`：克隆 O(N+E)；每个 `AddEdge` 环检测 O(N+E)；批次总复杂度 O(N+E + B·(N+E))，提交 O(1) 换指针。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N+E) 拷贝 + O(N log N + E log E) 排序。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
