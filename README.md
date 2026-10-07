# expirytable424

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

`Table` 以 `map[string]Entry` 作为主索引，按键 O(1) 定位条目；`ExpiresAt <= Now` 的到期判定在事务候选阶段与 `Expire` 中线性扫描完成。`Snapshot`/`Expire` 返回的切片按键排序，保证输出确定性。单把 `sync.RWMutex` 保护全部状态：写操作（`Apply`/`Expire`）取写锁，只读操作（`Snapshot`/`Stats`/`ValidateBatch`/`Clone`/`Preview` 的快照获取）取读锁，因此所有公开方法均可并发调用且可线性化。

### 候选事务

`Apply` 先执行与 `ValidateBatch` 共享的无副作用结构预检（`ErrInvalidInput` 优先），再检查单调时间（`ErrTime`）。随后在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision。只有在全部操作成功且最终容量不超限（`ErrCapacity`）时才整体提交；任何错误都会连同淘汰、逻辑时间和 revision 一起回滚。非空成功批次 generation 恰好加一，空批次不改变 generation。`Expire` 使用相同的闭区间边界。

### 所有权

所有返回的切片（`Snapshot.Entries`、`Expire` 结果）都是新分配的副本，与内部状态完全隔离；调用方修改返回值不影响表。`Clone` 深拷贝条目与逻辑时钟（now、generation、nextRevision），克隆体与原对象互不影响。`Preview` 在一次读锁内取得一致快照并克隆，在克隆体上复用完整 `Apply` 事务语义，返回候选 `Result`、`Snapshot`、`Stats`；原对象的状态、generation、revision 与逻辑时间均不变，失败时返回与 `Apply` 相同的错误且全部返回值为零值。

### 复杂度

设 n 为条目数、m 为批次操作数：结构校验 O(批次字节数)；`Apply` 为 O(n + m)（候选复制加顺序执行）；`Expire` 为 O(n)；`Snapshot`/`Clone` 为 O(n)（含排序 O(n log n)）；`Stats` 为 O(1)；`Preview` 等价于一次 `Clone` 加一次 `Apply`。空间开销为 O(n)，`Apply`/`Preview` 额外使用 O(n) 的候选副本。
