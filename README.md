# expirytable294

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## 设计说明

### 索引

表内条目存放在 `map[string]Entry` 哈希索引中，按键 O(1) 定位。`Snapshot`/`Expire` 返回的切片按字典序排序，保证确定性输出；返回切片均为新建副本，与内部状态完全隔离。

### 候选事务

`Apply` 先调用 `ValidateBatch` 做无副作用的完整结构校验（非负时间、已知 kind、键字符集与长度、Put/Touch 的 `ExpiresAt > Now`），再在互斥锁内检查单调时间。随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision。最终容量超限或任何错误（`ErrNotFound`/`ErrCapacity`）发生时，候选副本被直接丢弃，淘汰、时间与 revision 一并回滚；只有全部成功才提交并令 generation 加一。空批次成功但不产生任何变化。

### 所有权

`Clone` 在锁内深拷贝条目映射与逻辑时钟（now、generation、nextRevision），克隆体与原表互不影响。`Snapshot`、`Expire`、`Stats` 均只返回按值复制的数据，调用方无法通过返回值触及内部状态。

### 复杂度

- `New`/`Stats`：O(1)。
- `ValidateBatch`：O(批次操作数 × 键长)，不读状态。
- `Apply`：O(n + 批次操作数)，n 为当前条目数（候选复制与淘汰扫描）。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。

所有公开方法由单一互斥锁保护，可并发调用；`Stats` 与 `Snapshot` 在锁内读取，提供线性一致视图。
