# metacatalog236

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

`Store` 使用单一 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue` 运行计数，避免每次容量检查都全表扫描。`Snapshot`/`Result.Changed` 的名称排序在读取时按需进行，不维护有序索引。

### 候选事务

`Apply` 先在 `ValidateBatch` 中做无副作用的完整结构校验（不读状态），然后在写锁内把当前 map 复制为候选副本，按输入顺序在副本上执行 Put/Delete：Put 从 `nextRev` 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查。任一失败直接丢弃候选副本，状态、generation 与 revision 时钟天然回滚；成功时整体替换 map，generation 只增一次，空批次不变。

### 所有权

写入时复制调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回深拷贝，返回值与内部状态完全隔离。`Clone` 深拷贝全部记录与逻辑时钟（generation、nextRev），克隆体与原实例互不影响。

### 复杂度

- `Get`：O(1)（加 O(|Value|) 拷贝）；`Stats`：O(1)。
- `Apply`：O(n + m)，n 为现有记录数（候选复制），m 为批内操作数。
- `Snapshot`：O(n log n)（排序）；`Clone`：O(n)。
- 并发：单 `sync.RWMutex`，读路径（Get/Snapshot/Stats/Clone）共享读锁，写路径（Apply）独占写锁，`Stats` 与 `Clone` 对并发事务是线性一致的。
