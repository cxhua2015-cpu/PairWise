# resourcelease184

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Entry`，按键 O(1) 定位；`Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。
- 表内维护单调时间 `now`、单调计数器 `generation` 与 `nextRevision`（从 1 开始，下一个待分配的 revision）。

**候选事务（Apply）**
1. 先对整个批次做结构校验（kind 合法、键为非空 ASCII 小写字母/数字/`-`/`_` 且不超 `MaxKeyBytes`、时间非负），任何错误返回 `ErrInvalidInput`，且不触碰状态。
2. 再检查时间单调性（`Now < now` 返回 `ErrTime`）。
3. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision，Touch/Delete 目标缺失返回 `ErrNotFound`。
4. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。任何失败整体回滚：淘汰、时间与 revision 均不生效。
5. 提交时更新时间、revision 与条目；非空成功批次 `generation` 恰好加一，空批次不变。

**Expire** 使用相同闭区间边界（`ExpiresAt <= now`），推进表时间并返回被删除条目。

**所有权与并发**
- 所有公开方法由单一互斥锁保护，可并发调用。
- 内部 `Entry` 为值类型；`Snapshot` 与 `Expire` 返回新建切片，调用方修改不影响表内状态。

**复杂度**
- Put/Touch/Delete 单操作 O(1)；Apply 每批次 O(n + m)（n 为现存条目数，用于候选淘汰，m 为操作数）。
- Expire O(n)；Snapshot O(n log n)（排序）。空间 O(n)。
