# resourcelease124

并发安全的内存型“资源租约表”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。

## 索引

- 主索引为 `map[string]Entry`，以键直接定位条目，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot` 与 `Expire` 的结果按键排序返回，保证确定性输出。
- 未维护额外的过期堆；每批 Apply 先在候选状态上做一次 O(n) 淘汰扫描，换取实现的简单与回滚的可靠。

## 候选事务

`Apply` 分三段执行：

1. **结构校验**：在读取任何状态前完整校验整个批次（kind 合法、键为非空 ASCII 小写字母/数字/`-`/`_` 且不超 `MaxKeyBytes`、时间非负），失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`。
3. **候选执行**：复制当前条目集合为候选状态，先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision。最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误（含 `ErrNotFound`、`ErrCapacity`）都会整体回滚：淘汰、时间与 revision 计数器均不变。只有全部成功才一次性提交候选状态、推进时间，且非空批次 generation 恰好加一。`Expire` 使用相同的闭区间边界（`ExpiresAt <= now`）并推进单调时间。

## 所有权

- 所有公开方法通过一把 `sync.Mutex` 串行化，支持任意并发调用。
- 返回的 `[]Entry`/`Snapshot` 均为独立拷贝，调用方修改不会影响内部状态。
- 表实例独占其条目映射，无共享全局状态。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选条目的候选复制与淘汰扫描），m 为批次内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
