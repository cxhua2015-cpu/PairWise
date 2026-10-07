# readyqueue435

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引与排序
- 主索引为 `map[string]Item`（ID → 条目），Enqueue/Cancel 按 ID O(1) 定位。
- 不维护堆；`Pop` 与 `Snapshot` 在临界区内按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选切片排序，保证顺序确定且实现简单。

### 候选事务（Apply / Preview）
- `Apply` 先做完整结构校验（不读状态），再在写锁内把 `items` 复制为工作副本，顺序执行 Enqueue/Cancel，容量只在末尾检查；任一步失败直接丢弃工作副本，时间、状态与 revision 计数器天然回滚，无需补偿日志。
- `Preview` 通过 `Clone` 在一次读锁内取得线性化快照，在候选队列上复用同一份 `Apply` 事务语义，返回候选 `Result`/`Snapshot`/`Stats`；原对象、逻辑时钟与所有权完全不变，失败时返回与 `Apply` 相同的错误且其余返回值为零值。

### 所有权与隔离
- 所有返回切片（`Pop`、`Snapshot`、`Preview`）均为新建副本，调用方修改不会影响内部状态。
- `Clone` 深拷贝条目 map 与逻辑时钟（generation、nextRevision、now），克隆体与原对象互不影响。

### 并发与复杂度
- 单把 `sync.RWMutex` 保护全部状态；`Apply`/`Pop` 取写锁，`Snapshot`/`Stats`/`Clone`/`Preview` 取读锁，`ValidateBatch` 无锁纯函数。
- 复杂度（n = 队列大小，k = 批次大小，m = 就绪数）：`Apply` O(n + k)，`Pop` O(n + m log m)，`Snapshot`/`Clone`/`Preview` O(n log n) / O(n) / O(n + k)，`Stats`/`ValidateBatch` O(1) / O(k)。
