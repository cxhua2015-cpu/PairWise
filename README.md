# expirytable399

并发安全的内存型“到期状态表 399”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。

## 索引

- 主索引为 `map[string]Entry`，按 key 存取，Put/Touch/Delete 均为 O(1)。
- 未维护按过期时间排序的辅助索引；淘汰与 `Expire` 采用全表扫描，O(n)。条目数受 `Options.MaxEntries` 约束，扫描成本有界。
- `Snapshot` 与 `Expire` 返回的条目按 key 排序，保证输出确定性。

## 候选事务

`Apply` 分三阶段：

1. **结构校验**：完整校验整个批次（Now 非负、kind 合法、key 字符集与长度、ExpiresAt 非负），不读取任何状态；失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`；空批次在校验后直接成功返回，不改变任何状态。
3. **候选执行**：在克隆的候选状态上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个单调递增的 revision。Touch/Delete 缺失键返回 `ErrNotFound`，最终条目数超限返回 `ErrCapacity`。

任何错误都直接丢弃候选状态，淘汰、时钟与 revision 分配随之整体回滚；只有全部成功才提交，且非空成功批次 generation 恰好加一。`Expire` 使用相同的闭区间边界 `ExpiresAt <= now`。

## 所有权

- 所有公开方法持有同一把互斥锁，可任意并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选克隆与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
