# metacatalog241

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
- 主索引为 `map[string]Record`（按名称哈希定位），Get/Delete 判定为 O(1) 均摊。
- `Snapshot` 与 `Result.Changed` 在读取时按名称排序（O(n log n)），不维护额外的有序结构，写入路径保持 O(1)。

### 候选事务（candidate transaction）
- `Apply` 先调用与 `ValidateBatch` 共享的无副作用结构预检（`validation.go`），此时不读取任何状态。
- 随后在写锁内把当前记录表浅拷贝为候选 map，按输入顺序执行 Put/Delete：Put 从候选逻辑时钟分配连续 revision，Delete 不分配 revision、缺失即 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末对候选状态检查，越限返回 `ErrCapacity`。
- 任何失败直接丢弃候选 map，原始状态、generation 与 nextRevision 完全不变；成功时整体替换记录表，非空批次 generation 恰好加一，空批次不改变任何时钟。

### 所有权
- Put 的 Value 在写入前深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态隔离。
- `Clone`（`clone.go`）复制全部记录与逻辑时钟（generation、nextRevision），克隆体与原实例不共享任何可变内存，可独立并发使用。

### 并发与统计
- 全部公开方法通过单个 `sync.RWMutex` 保护：写事务持写锁，`Get`/`Snapshot`/`Stats`/`Clone` 持读锁。
- `Stats`（`stats.go`）在锁内汇总，是线性一致的状态快照。

### 复杂度
- 结构预检：O(L)，L 为批次总输入字节。
- `Apply`：O(n + m)，n 为现有记录数（候选拷贝），m 为批次 op 数；外加 Changed 排序 O(k log k)，k 为涉及的不同名称数。
- `Get` O(1) 均摊；`Snapshot`/`Clone` O(n log n) / O(n)；`Stats` O(n)。
