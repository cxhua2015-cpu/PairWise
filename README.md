# metacatalog256

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录；`Record` 内联存储 Value 与 Revision。
- `Snapshot`/`Apply` 的 `Changed` 在读取时按名称排序（O(n log n)），不维护额外有序结构，以换取写入路径的常数复杂度。

### 候选事务
- `Apply` 先调用 `ValidateBatch` 做无副作用的完整结构校验（未知 kind、非法名称、超长 Value、Delete 携带 Value 均返回 `ErrInvalidInput`），不读取任何状态。
- 通过校验后在写锁内把当前记录复制为候选 map，按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配且目标缺失即 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。
- 任何失败直接丢弃候选 map，状态、generation、revision 全部不变；成功时整体换入候选 map，generation 只增一次，空批次完全不变。

### 所有权
- 入参 Value 在写入前拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改不会影响内部状态。
- `Clone` 在读锁下复制全部记录与逻辑时钟（generation、nextRevision），克隆体与原 Store 完全独立。

### 并发与复杂度
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot`/`Stats`/`Clone` 取读锁，统计与快照是线性一致的。
- Put/Delete 单操作 O(1)；批次 O(k + n)（k 为操作数，n 为记录数，含候选复制与容量求和）；`Snapshot`/`Clone` O(n log n)/O(n)。
