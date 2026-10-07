# expirytable434

并发安全的内存型到期状态表，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `heartbeat.go` — 公开类型、错误值与核心事务引擎（`New`/`Apply`/`Expire`/`Snapshot`）。
- `validation.go` — 无副作用的批次结构预检，`Apply` 与 `ValidateBatch` 共享同一套结构语义。
- `stats.go` — 线性一致的状态统计。
- `clone.go` — 保留逻辑时钟（now/generation/nextRevision）的深拷贝。
- `preview.go` — 在一致快照上复用完整 `Apply` 语义的事务预演。

## 索引

条目存储在 `map[string]Entry` 中，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片按需按 key 排序，保证确定性输出。单把 `sync.Mutex` 保护全部状态，所有公开方法可并发调用且各自线性化。

## 候选事务

`Apply` 先做完整结构校验（kind、键字符集与字节上限、`ExpiresAt > Now`），再检查单调时间；随后在候选 map 副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）。任何错误（含最终容量检查 `ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才提交。非空成功批次 generation 恰好加一，空批次不变。

## 所有权

`Snapshot`、`Expire`、`Preview` 返回的切片均为新建内存，与内部状态完全隔离；`Clone` 复制全部条目与逻辑时钟，克隆体与原对象互不影响。`Preview` 在锁内克隆出一致快照，再对克隆体执行真实 `Apply`，因此 Result/Snapshot/Stats 与同状态实际提交完全一致，而原对象的状态、generation、revision 与逻辑时间保持不变；失败时返回零值和与 `Apply` 相同的错误。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`：O(1)。`ValidateBatch`：O(m·L)，L 为键长。
