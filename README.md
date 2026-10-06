# metacatalog226

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete：Put 分配连续 revision，Delete 不分配；失败整体回滚状态与逻辑时钟。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New` / `Apply` / `Get` / `Snapshot`。
- `validation.go` — 无副作用的批次预检 `ValidateBatch`，与 `Apply` 共享同一套
  结构语义（kind 合法、名称字符集与长度、Value 长度上限、Delete 不得携带 Value）。
- `stats.go` — 线性一致的状态统计 `Stats`，所有字段在同一把读锁内读取。
- `clone.go` — 保留逻辑时钟（generation、nextRevision）的深拷贝 `Clone`。

## 设计说明

**索引**：记录存放在 `map[string]record` 哈希索引中，按名称 O(1) 定位；
`record` 内联存储 value 与 revision。`totalValue` 作为冗余聚合随事务增量维护，
使容量判定无需遍历。

**候选事务**：`Apply` 先调用 `ValidateBatch` 做完整结构校验（不读状态），然后在
写锁内把当前 map 浅拷贝为候选副本，按输入顺序在其上执行 Put/Delete。Delete 缺失
键返回 `ErrNotFound`；记录数与 Value 总字节容量只在批次末检查，超限返回
`ErrCapacity`。任何失败直接丢弃候选副本，已提交状态、generation 与 revision
天然不受影响；成功时整体换入候选副本，generation 只增加一次（空批次不变）。

**所有权**：所有进出边界的 Value 都做拷贝——Put 时拷入，`Get` / `Snapshot` /
`Result.Changed` 拷出，`Clone` 全量深拷贝。调用方对返回切片的任何修改都不会
影响目录内部状态，克隆体与原体不共享任何可变内存。

**复杂度**（n = 记录数，k = 批次数，B = 涉及 Value 总字节）：
- `Apply`：O(n + k·B) 时间与 O(n) 额外空间（候选副本），排序 `Changed` 为 O(k log k)。
- `Get`：O(B)（仅拷贝单个 Value）。
- `Snapshot` / `Clone`：O(n·B)；`Snapshot` 另需 O(n log n) 按名称排序。
- `Stats`：O(1)，读锁下单点一致快照。
- `ValidateBatch`：O(k·B)，不触碰共享状态，无锁。

**并发**：单把 `sync.RWMutex` 保护全部状态；写事务互斥，读操作（`Get` /
`Snapshot` / `Stats` / `Clone`）共享读锁，全部公开方法可并发调用。
