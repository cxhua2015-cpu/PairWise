# expirytable429

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

条目存储在以键为索引的 `map[string]Entry` 哈希索引中，按键 O(1) 定位。`Snapshot` 与 `Expire` 返回的切片按键排序，保证输出确定性；返回切片均为新建副本，与内部索引完全隔离。

### 候选事务

`Apply` 先在 `ValidateBatch` 中做无副作用的完整结构校验（时间非负、kind 合法、键字符集与字节上限、Put/Touch 的 `ExpiresAt > Now`），再在单把互斥锁内检查单调时间。随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision，最后做容量检查。任一步失败（`ErrTime`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，淘汰、逻辑时钟与 revision 全部回滚；只有非空成功批次才使 generation 递增一次。

### 所有权

`Clone` 在锁内深拷贝全部条目与逻辑时钟（now/generation/revision），克隆体与原对象不共享任何可变状态。`Preview` 在一致快照（一次克隆）上复用完整 `Apply` 事务语义，返回候选 `Result`、`Snapshot`、`Stats`，错误及优先级与同状态 `Apply` 一致，且原对象、逻辑时钟与所有权不变；失败时全部返回值为零值。`Stats` 与 `Snapshot` 在锁内读取，提供线性一致视图。

### 复杂度

设批次含 `k` 个操作、表内 `n` 个条目：`ValidateBatch` 为 O(k·L)（L 为键长）；`Apply` 为 O(n + k)（候选复制与淘汰扫描）；`Expire`、`Snapshot` 为 O(n log n)（排序）；`Stats` 为 O(1)；`Clone`/`Preview` 为 O(n + k)。所有公开方法通过单把互斥锁保证并发安全。
