# expirytable439

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 索引与数据结构

- 主索引为 `map[string]Entry`，按键精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- 快照与 `Expire` 结果按 key 排序输出，保证确定性；排序为 O(n log n)。
- 键合法性（非空、`[a-z0-9-_]`、不超过 `MaxKeyBytes`）在结构校验阶段以 O(key 长度) 完成。

## 候选事务

- `Apply` 先调用 `ValidateBatch` 做无副作用的完整结构校验，再检查单调时间（`Now < now` 返回 `ErrTime`）。
- 在候选副本上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision。
- 最终容量检查（`MaxEntries`）在提交前进行；任何错误都会连同淘汰、时间与 revision 一起回滚，原状态不变。
- 非空成功批次 generation 恰好 +1；空批次为完全 no-op，不推进时钟与 generation。

## 所有权与并发

- 所有公开方法通过单个互斥锁线性化，可并发调用。
- 所有返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的拷贝，与内部状态隔离。
- `Clone` 深拷贝全部状态（含 generation、nextRevision、now），克隆体与原对象互不影响。
- `Preview` 在持锁的一致快照上克隆候选表并复用完整 `Apply` 事务语义，返回候选 `Result`/`Snapshot`/`Stats`；错误及优先级与同一状态上的 `Apply` 完全一致，失败时返回零值，且绝不改变原对象。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数。
- `Snapshot`/`Clone`/`Preview`：O(n log n) / O(n) / O(n + m + n log n)。
- `Stats`：O(1)。
