# controlgraph113

并发安全的内存型“控制面依赖图 113”。实现见 `controlgraph113/trustgraph.go`，规范见 `SPEC.md`。

## 设计

**索引**
- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 去重与删除。
- 未维护邻接表；可达性检查时按需从边集合构建临时邻接表，换取写路径的简洁与去重自由。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind 合法、字段形状、名称字符集与字节上限），不读取任何状态，失败返回 `ErrInvalidInput`。
- 校验通过后在写锁内把节点/边两个 map 浅拷贝为候选副本，按顺序在副本上执行全部 op；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃副本，已提交状态不受影响。
- 节点/边容量只在批次末尾对候选副本检查，超限返回 `ErrCapacity` 并整体回滚；因此批次中途可以暂时超过容量（如先删后增）。
- 全部通过后用候选副本原子替换提交状态，非空成功批次 `generation` 恰好加一；空批次与失败批次不改变 generation。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁（`sync.RWMutex`）。
- `Reachable` 在读锁内对当前一致快照做 DFS；`Snapshot` 在锁内拷贝后对节点按字典序、边按 `(From, To)` 稳定排序。
- 返回的切片均为新分配的副本，调用方修改不会污染内部状态；提交后不再被引用的旧 map 由 GC 回收。

**环检测**
- 加边 `from -> to` 前在候选副本上检查 `to` 是否已可达 `from`（含 `from == to` 自环），可达则返回 `ErrCycle`，保证图始终是有向无环图。
- 删除节点会级联删除其所有入边与出边。

## 复杂度

设 N 为节点数、E 为边数、K 为批次内 op 数：

- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选拷贝 O(N + E)；每个 AddEdge 的环检测 O(N + E)；整体 O(N + E + K·(N + E))，空间 O(N + E)。
- `Reachable`：O(N + E) 时间，O(N + E) 临时空间。
- `Snapshot`：O(N + E) 拷贝 + O(N log N + E log E) 排序。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
