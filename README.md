# expirytable389

并发安全的内存型“到期状态表”，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（按键 O(1) 定位），配单一 `sync.Mutex` 保护全部内部状态。
- 不维护堆等额外到期索引：`Apply`/`Expire` 的淘汰是对候选/现有 map 的一次线性扫描，以简洁换取可预测的内存占用。
- `Snapshot` 与 `Expire` 返回的条目按键名字典序排序，保证输出确定性。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：完整校验批次（Now 非负、Kind 合法、键非空且仅含 `[a-z0-9-_]` 且不超 `MaxKeyBytes`、`ExpiresAt` 非负），不读取任何状态，失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`（显式非负单调时间，允许相等）。
3. **候选执行**：把未到期（`ExpiresAt > Now`）的条目克隆进候选 map，顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRev` 分配 revision。Touch/Delete 缺失键返回 `ErrNotFound`，最终条目数超 `MaxEntries` 返回 `ErrCapacity`。

任何错误都直接丢弃候选——淘汰、时间推进和 revision 分配随之一并回滚，内部状态保持逐位不变。只有全部成功才提交候选、推进 `now` 并将 `generation` 恰好加一；空批次为完全无操作（generation 不变）。`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now` 删除）并推进时间。

## 所有权

- 表独占内部条目；`Snapshot`/`Expire` 返回的切片与 `Entry` 值均为新分配的拷贝，调用方可自由修改，不影响表内状态。
- 输入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。

## 复杂度

设 n 为当前条目数、b 为批次操作数：

- `Apply`：时间 O(n + b)，空间 O(n)（候选克隆）。
- `Expire`：O(n)。
- `Snapshot`：O(n log n)（排序）。
- 所有公开方法持有同一把互斥锁，可安全并发调用；锁内无阻塞操作。
