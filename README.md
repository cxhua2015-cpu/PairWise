# metacatalog421

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**: 记录存储在以名称为键的 `map[string]Record` 哈希索引中，`Get` 为 O(1)；`Snapshot`/`Result.Changed` 在读取时对键排序，按名称字典序输出。单把 `sync.RWMutex` 保护全部状态，写操作独占、读操作共享，保证所有公开方法可并发调用且可线性化。
- **候选事务**: `Apply` 先在副本 map 上按输入顺序重放 Put/Delete（Put 预分配连续 revision，Delete 不分配），仅在批次末检查记录数与 Value 总字节容量；任一步失败直接丢弃副本，状态、generation 与 revision 全部回滚。`Preview` 通过 `Clone` 取得一致快照，在候选 `Store` 上复用同一 `Apply` 语义，返回候选 `Result`/`Snapshot`/`Stats`，原对象与逻辑时钟不变；失败时返回与 `Apply` 相同的错误且全部返回值为零值。
- **所有权**: 写入时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result`/`Clone`/`Preview` 返回的所有切片均为深拷贝，返回值与内部状态及候选状态完全隔离。
- **复杂度**: 结构校验 O(批次总字节)；`Apply` 为 O(现有记录数 + 批次大小)（提交前复制索引）加 O(k log k) 的结果排序；`Get` O(1)；`Snapshot`/`Stats`/`Clone`/`Preview` 为 O(n log n)/O(n)（n 为记录数）。
