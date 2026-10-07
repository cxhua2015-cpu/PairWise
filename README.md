# expirytable399

并发安全的内存型“到期状态表 399”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位；条目内联存储 `ExpiresAt` 与 `Revision`。
- `Snapshot` 与 `Expire` 的返回切片按键名排序，保证确定性输出；返回的是拷贝，调用方修改不影响表内状态。
- 未维护额外的堆/时间轮：每次 `Apply` 在候选状态上做一次 O(n) 的过期清扫（`ExpiresAt <= Now`，闭区间），`Expire` 同样为 O(n)。在目标规模（控制面元数据、容量受 `MaxEntries` 约束）下，这避免了第二索引的一致性成本。

## 候选事务

`Apply` 分三个阶段，全程持锁：

1. **结构校验**：完整校验整个批次（`Now >= 0`、kind 合法、键为非空 ASCII 小写/数字/`-`/`_` 且不超过 `MaxKeyBytes`、`ExpiresAt >= 0`），失败返回 `ErrInvalidInput`，不读取任何状态。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`（显式非负单调时间）。
3. **候选执行**：复制当前条目为候选 map，先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision，Touch/Delete 缺失键返回 `ErrNotFound`，最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都直接丢弃候选 map——淘汰、时间与 revision 计数器随之整体回滚，无需补偿日志。只有全部成功才一次性提交：更新时间、条目、revision 计数器，且非空批次 `generation` 恰好加一（空批次不变，但仍推进时间）。`Expire` 使用相同的闭区间边界并推进时间。

## 所有权

- `New` 校验 `Options`（上限必须为正，否则 `ErrInvalidOptions`）后，配置不可变。
- 表独占内部 map；`Snapshot().Entries` 与 `Expire` 返回的切片均为新分配的拷贝，与内部状态隔离。
- `Batch`/`Op` 按值传入，实现不保留调用方切片引用。

## 并发与复杂度

- 所有公开方法通过单一 `sync.Mutex` 串行化，可安全并发调用；无锁内分配之外的共享状态。
- `Apply`：O(n + m)，n 为当前条目数（候选复制 + 清扫），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
