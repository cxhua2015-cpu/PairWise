# metacatalog246

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue` 运行计数，使批次末的总字节容量检查为 O(1)。`Snapshot` 与 `Result.Changed` 在读取时按名称排序，不在写入路径上维护有序结构。

### 候选事务

`Apply` 先在 `ValidateBatch` 中做完整结构预检（不读取任何状态），随后在写锁内把当前索引浅拷贝为候选 map，按输入顺序在其上执行 Put/Delete：Put 从单调递增的 `nextRevision` 分配连续 revision，Delete 不分配。记录数与 Value 总字节上限只在批次末检查；任何失败直接丢弃候选，状态、generation 与 revision 全部回滚。非空成功批次 generation 恰好加一。

### 所有权

写入时复制调用方传入的 Value；`Get`、`Snapshot`、`Result.Changed` 均返回深拷贝；`Clone` 逐条复制 Value 并保留 generation 与 nextRevision 逻辑时钟，克隆体与原Store完全独立。返回的切片与内部状态无任何共享内存。

### 并发与复杂度

所有公开方法通过 `sync.RWMutex` 并发安全：读路径（`Get`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）使用读锁，`Apply` 使用写锁，统计结果线性一致。设批次含 B 个操作、目录含 N 条记录：`Apply` 为 O(N + B + C log C)（C 为变更名数），`Get` 为 O(V)（V 为值长度），`Snapshot`/`Clone` 为 O(N log N + 总字节数)，`Stats` 为 O(1)，`ValidateBatch` 为 O(B)。
