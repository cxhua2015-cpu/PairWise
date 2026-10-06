# expirytable224

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot`/`Expire` 返回的切片按键排序，保证确定性输出，且与内部 map 完全隔离。

### 候选事务
- `Apply` 先在 `validation.go` 的 `ValidateBatch` 中做无副作用的完整结构校验（时间非负、kind 合法、键字符集与字节上限、Put/Touch 的 `ExpiresAt > Now`），再在锁内做单调时间检查。
- 随后在候选副本（原 map 的拷贝）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配版本。
- 最终容量超限或任何错误（`ErrNotFound`/`ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才提交并令 generation 恰好加一（空批次不变）。

### 所有权
- 所有公开方法由单把互斥锁保护，可并发调用；`Stats` 在锁内读取，是线性一致快照。
- `Clone` 在锁内深拷贝 map 与逻辑时钟（now/generation/nextRevision），克隆体与原表互不影响。
- 返回值（`Snapshot.Entries`、`Expire` 结果）均为新建切片，调用方修改不会污染表内状态。

### 复杂度
- Put/Touch/Delete 单操作 O(1) 均摊；`Apply` 为 O(n + m)（n 为现存条目数，m 为批次操作数）。
- `Expire` O(n)，`Snapshot`/`Clone` O(n log n)/O(n)，空间 O(n)。
