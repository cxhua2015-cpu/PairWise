# readyqueue275

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引与规范顺序
核心索引是 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。Pop 与 Snapshot 在持锁状态下把候选收集成切片，再按规范顺序排序：**Priority 降序、ReadyAt 升序、ID 升序**。Pop 只选取 `ReadyAt <= now` 的条目，截取前 `limit` 个并在同一临界区内原子删除。

### 候选事务（Apply）
`Apply` 先调用与 `ValidateBatch` 共享的无副作用结构预检（非负时间、合法 kind、ID 字符集与字节上限、Cancel 不得携带 Priority/ReadyAt），再检查单调时间。随后在**候选副本**上顺序执行 Enqueue/Cancel：Enqueue 从逻辑时钟分配递增 revision，Cancel 可作用于本批次先前入队的条目。容量上限只在末尾检查一次。任何一步失败（ErrExists / ErrNotFound / ErrCapacity / ErrTime）都会丢弃候选，时间、状态与 revision 时钟完全回滚；全部成功才一次性提交。非空成功批次 generation 恰好 +1，空批次不变。

### 统计与克隆
`Stats` 与 `Snapshot` 在同一互斥锁下读取，返回线性一致的状态摘要；`Clone` 深拷贝索引与全部逻辑时钟（now、generation、nextRevision），克隆体与原队列完全隔离、可独立演进。

### 所有权
所有公开方法返回的切片与 `Item` 都是新分配的副本，调用方修改返回值不会影响队列内部状态。

### 复杂度
- `Apply`：O(n + k)，n 为当前条目数（候选复制），k 为批内操作数。
- `Pop`：O(n log n)，n 为就绪候选数（排序主导）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `ValidateBatch`：O(k)，纯函数式预检，不读状态。
- 并发安全由单一互斥锁保证，所有公开方法可安全并发调用。
