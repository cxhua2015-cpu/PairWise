# expirytable229

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：核心状态为 `map[string]Entry` 哈希索引，键查找/插入/删除均为 O(1)；`Snapshot`/`Expire` 返回的切片按键排序，保证确定性输出且与内部状态完全隔离。
- **候选事务**：`Apply` 先调用 `ValidateBatch` 做无副作用的完整结构校验（共享同一套结构语义），再在互斥锁内检查单调时间，随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目、顺序执行 Put/Touch/Delete 并分配 revision。容量终检或任何错误直接丢弃候选，淘汰、时间与 revision 随之一并回滚；只有全部成功才一次性提交，generation 对非空批次恰好加一。
- **所有权**：`Clone` 在锁内深拷贝全部条目与逻辑时钟（generation/nextRevision/now），克隆体与原表无任何共享内存；`Snapshot`、`Expire` 返回的切片均为新建副本，调用方修改不影响表。
- **复杂度**：Put/Touch/Delete 单操作 O(1)；`Apply` 为 O(n + m)（n 为现存条目数，用于候选淘汰，m 为批内操作数）；`Expire`、`Snapshot`、`Clone` 为 O(n)；`Stats`、`ValidateBatch` 分别为 O(1) 与 O(m)。所有公开方法通过单一互斥锁实现并发安全与线性一致。
