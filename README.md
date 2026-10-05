# resourcecatalog141

Read `SPEC.md` and implement the package.

本任务要求状态引擎、policy.go 与 coordinator.go 三个生产文件协同实现，详见 SPEC.md。

## 架构说明

### 状态引擎（servicecatalog.go）
- **索引**：记录存储在以名称为键的 `map[string]Record` 中，Get/Put/Delete 均为 O(1) 均摊查找；另维护 `totalValue` 运行计数，避免每次容量检查重新求和。
- **候选事务**：`Apply` 先在候选 map（现有记录的浅拷贝）上按输入顺序执行整批操作，Put 从单调递增的 `nextRevision` 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查；任何失败直接丢弃候选，已提交状态、generation 与 revision 完全不变（回滚零成本）。仅在全部成功时一次性替换内部 map、generation++ 并推进 revision。
- **所有权**：写入时深拷贝输入 Value；`Get`/`Snapshot`/`Result.Changed` 返回的记录均含独立 Value 副本，调用方后续修改不会影响内部状态，反之亦然。Snapshot 与 Changed 按名称排序。
- **复杂度**：Apply 为 O(n + k log k)（n 为现有记录数的候选拷贝，k 为变更名排序）；Get 为 O(1) 均摊；Snapshot 为 O(m log m)（m 为记录数）。单把互斥锁保证所有公开方法并发安全。

### 策略层（policy.go）
- 独立的 `sync.RWMutex` 保护 actor 白名单（`map[string]struct{}`）与单批操作数上限 `maxOps`。
- `ReplaceActors` 先构建新集合再原子替换，替换期间 `Authorize` 读锁不受阻塞；不在白名单或操作数超限返回 `ErrDenied`。

### 协调层（coordinator.go）
- `Apply` 先调用 `Policy.Authorize`（拒绝时不触碰核心状态），通过后才委托 `Store.Apply`。
- 每次尝试（成功、拒绝、引擎失败）都在独立互斥锁下追加一条 `Decision`，序号从 1 连续单调分配；`Decisions()` 返回内部切片的独立拷贝，与内部存储无别名。
