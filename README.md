# expirytable369

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位。
- 不维护额外的按时间排序索引：`Apply`/`Expire` 的淘汰扫描为 O(n)，`Snapshot` 返回按字典序排序的副本（O(n log n)），保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：先校验全部 Op 的 kind 与键（非空、ASCII 小写字母/数字/连字符/下划线、不超过 `MaxKeyBytes`），再检查时间单调性（`Now < 当前时间` 返回 `ErrTime`）。此阶段不读取、不修改状态。
2. **候选执行**：在条目 map 的私有副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个单调递增的 revision。最终容量超限（`ErrCapacity`）或任何错误（`ErrNotFound` 等）都会整体回滚——淘汰、时间与 revision 分配一并作废，已存状态不变。

非空成功批次 `generation` 恰好加一；空批次成功但 `generation` 不变。`Expire` 使用相同的 `ExpiresAt <= now` 闭区间边界，并推进表内时间。

## 所有权

- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由单一互斥锁保护，可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响内部状态；传入的 `Batch`/`Op` 不会被保留或修改。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（复制 + 淘汰扫描），m 为批次内 Op 数。
- `Expire`：O(n + k log k)，k 为到期条目数（结果排序）。
- `Snapshot`：O(n log n)（排序副本）。
- 空间：O(n)。
