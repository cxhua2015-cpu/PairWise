# topologygraph423

并发安全的内存型“控制拓扑图 423”，Go 1.22+，仅依赖标准库。公开契约见 `SPEC.md` 与 `topologygraph423/contract_test.go`。

## 架构（多文件联动）

- `trustgraph.go` — 核心事务引擎：`Graph`、`New`、`Apply`、`Reachable`、`Snapshot`，以及候选状态 `state` 上的单操作语义（`applyOp`）与可达性。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`：名称字符集/字节上限、未知 kind、额外字段、自环。`Apply` 与 `Preview` 复用同一套结构语义。
- `stats.go` — 线性一致的 `Stats`（generation、节点数、边数）。
- `clone.go` — 保留逻辑时钟（generation）且所有权完全隔离的深拷贝 `Clone`。
- `preview.go` — 事务预演 `Preview`：在一次线性化快照上复用完整 `Apply` 语义，返回候选 `Result`/`Snapshot`/`Stats`，错误及优先级与 `Apply` 一致，失败时全部返回零值；原对象状态、generation 与逻辑时钟不变。

## 索引

图状态由两个哈希索引组成：`nodes map[string]struct{}`（节点存在性 O(1)）与 `edges map[Edge]struct{}`（边存在性 O(1)，键为有序对 `(From, To)`）。边不建邻接表；可达性与按点删边通过一次边表扫描完成，以换取实现简单与无冗余索引不一致风险。

## 候选事务

`Apply`/`Preview` 先在当前状态上做一次深拷贝得到候选状态，将全部操作按序作用于候选；任一操作失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败（`ErrCapacity`）时直接丢弃候选，实现整体回滚。只有全部成功才用候选替换当前状态，且非空成功批次 generation 只递增一次，空批次不变。容量只在批次末检查，因此“先删后增”的替换型批次可以成功。

## 所有权

所有返回的切片（`Snapshot.Nodes`、`Snapshot.Edges`）都是新建并排序后的副本；`Clone` 深拷贝全部 map；`Preview` 的候选快照/统计来自候选状态而非原对象。调用方修改返回值不会影响图的内部状态，反之亦然。

## 并发

单个 `sync.RWMutex` 保护状态与逻辑时钟：写路径（`Apply`）取写锁，读路径（`Reachable`、`Snapshot`、`Stats`、`Clone`、`Preview`）取读锁。`Preview` 在读锁内基于候选副本计算，绝不写回接收者，因此多个 `Preview` 与只读方法可并发执行。

## 复杂度

设 N 为节点数、E 为边数、K 为批次操作数：

- `ValidateBatch`：O(K · L)，L 为名称长度上限；不读取状态。
- `Apply`：O(N + E) 候选拷贝 + 每操作 O(1) 均摊（`AddEdge` 的环检测为一次 BFS，O(N + E)；`DeleteNode` 关联边清理 O(E)）+ 批次末容量检查 O(1)。
- `Reachable`：BFS，O(N + E)。
- `Snapshot`：O(N log N + E log E) 稳定排序。
- `Stats`：O(1)。`Clone`：O(N + E)。`Preview`：与 `Apply` 同阶，外加一次快照排序。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
