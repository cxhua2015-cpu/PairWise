# balanceledger287

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位；`Account` 为值类型，存入即拷贝。
- 未维护有序索引：`Top` 与 `Snapshot` 在读取时拷贝全部账户并排序（`Top` 按数值降序、名称升序；`Snapshot` 按名称升序），返回切片与内部状态完全隔离。

### 候选事务
- `Apply` 先做无副作用的结构预检（与 `ValidateBatch` 共享 `validateBatch`），再在写锁内把 `accounts` 复制为候选 map，按输入顺序在其上执行 Add/Set/Delete。
- Add/Set 在候选上分配连续 revision；算术前用 `math.MaxInt64/MinInt64` 边界比较检测 int64 溢出，并执行 `MaxAbsValue` 绝对值上限；最终账户容量仅在批次末尾检查。
- 任一步失败直接丢弃候选，状态、generation、revision 时钟完全不变，实现整体回滚；成功时一次性换入候选，非空批次 generation 只加一。

### 所有权
- 所有公开方法经 `sync.RWMutex` 串行化读写，并发安全且线性一致。
- `Clone` 在读锁下深拷贝 map 与逻辑时钟（generation、nextRevision），克隆体与原账本无任何共享内存。
- `Top`/`Snapshot`/`Result.Changed` 均返回新建切片，调用方修改不影响账本。

### 复杂度
- `Apply`：O(B + A)，B 为批内 op 数，A 为当前账户数（候选复制）。
- `ValidateBatch`：O(B·L)，L 为名称长度；不读状态。
- `Top`/`Snapshot`：O(A log A)；`Stats`：O(1)；`Clone`：O(A)。
