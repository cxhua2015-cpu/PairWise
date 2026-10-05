# controlgraph128

并发安全的内存型“控制面依赖图 128”（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点
级联删除关联边，节点/边容量只在批次末检查，失败整体回滚。

## 设计

### 索引
图状态由三张索引组成：

- `nodes map[string]struct{}`：节点集合。
- `out map[string]map[string]struct{}`：出边邻接表（from → to 集合）。
- `in  map[string]map[string]struct{}`：入边反向索引（to → from 集合）。

`out`/`in` 冗余存储使 `DeleteNode` 级联删除和 `DeleteEdge` 都是 O(度数) 而无需
全图扫描；`Snapshot` 从 `out` 重建边列表并对节点与边做稳定字典序排序。

### 候选事务（candidate transaction）
`Apply` 分两阶段：

1. **结构校验**：在不读取任何状态的前提下校验全部 op（kind 合法、节点 op 不带
   `To`、名称非空且仅含 `[a-z0-9-_]`、长度 ≤ `MaxNameBytes`），任何失败返回
   `ErrInvalidInput`。
2. **候选提交**：在写锁内把当前状态深拷贝为候选状态，顺序应用所有 op
   （存在性 → `ErrExists`，缺失 → `ErrNotFound`，成环 → `ErrCycle`），最后
   一次性检查 `MaxNodes`/`MaxEdges`（超限 → `ErrCapacity`）。任何失败直接
   丢弃候选，原状态零改动，实现整体回滚；成功后原子替换状态并将
   `generation` 加一。空批次不修改 generation。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁
  （`sync.RWMutex`），因此 `Reachable` 总是基于当前一致快照。
- 内部状态绝不外泄：`Snapshot` 返回新建的切片；调用方修改返回值不影响图。
- 环检测与 `Reachable` 复用同一 BFS，均沿 `out` 索引遍历。

### 复杂度
设 V 为节点数、E 为边数、B 为批次大小：

- `Apply`：O(V + E) 深拷贝 + 每 op O(1) 均摊；每次 `AddEdge` 的环检测为
  O(V + E) BFS；容量检查 O(1)。
- `Reachable`：O(V + E) BFS。
- `Snapshot`：O(V log V + E log E) 排序。
- 空间：O(V + E)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
