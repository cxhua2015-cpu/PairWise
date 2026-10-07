# topologygraph343

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 查重与删除。
- 邻接表不持久化；可达性检查时按需从边集合临时构建，避免双索引不一致。

## 候选事务（原子批次）

`Apply` 分两阶段：

1. **结构校验**：先完整校验所有 op 的 kind、名称字符集（`[a-z0-9_-]`、非空、≤ `MaxNameBytes` 字节）与多余字段，不读取任何状态；失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把节点/边映射克隆为候选副本，按序应用全部 op（存在性、环检测等语义错误立即失败）；仅在批次末尾检查最终节点/边容量。任何失败直接丢弃候选副本，即整体回滚；全部成功才一次性换入并令 `generation` 加一（空批次不变）。

## 所有权与并发

- `Graph` 内部由 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，所有公开方法可并发调用。
- `Snapshot` 返回新建切片（节点升序、边按 `(From, To)` 升序稳定排序），与内部状态完全隔离；调用方修改返回值不影响图。
- 环检测在候选边集上执行，因此并发批次之间不会观察到中间态。

## 复杂度

设 N 为节点数、E 为边数、K 为批次 op 数：

- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选克隆 O(N+E)；每个 `AddEdge` 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。总计 O(K·(N+E))。
- `DeleteNode`：级联删除关联边需扫描边集，O(E)。
- `Reachable`：O(N+E)（临时邻接表 + DFS）。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
