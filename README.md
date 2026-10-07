# expirytable319

并发安全的内存型“到期状态表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位。
- 不维护堆等按时间排序的辅助索引；到期扫描为全表遍历，换取 Put/Touch/Delete 的 O(1) 写入与实现的简洁性。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

## 候选事务（Apply）

1. 先对整个批次做结构校验（时间非负、kind 合法、键字符集与长度上限），再读取任何状态。
2. 检查单调时间：`Now` 不得小于当前表时间，否则 `ErrTime`。
3. 在候选状态（条目的拷贝）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 缺失键报 `ErrNotFound`。
4. 最终条目数超过 `MaxEntries` 报 `ErrCapacity`。
5. 任何错误都整体回滚：淘汰、时间、revision、generation 均不变；只有全部成功才提交候选状态。非空成功批次 generation 恰好加一，空批次不变。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`），推进表时间并返回被淘汰条目，不影响 generation 与 revision。

## 所有权与并发

- 所有公开方法由单一互斥锁保护，可并发调用。
- 返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的拷贝，与内部状态完全隔离；调用方可自由修改。
- `Entry`/`Result`/`Snapshot` 为值类型，按值返回，无共享引用。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（n 条目、m 个 op） | O(n + m) | O(n) 候选拷贝 |
| `Expire` | O(n + k log k)，k 为淘汰数 | O(k) |
| `Snapshot` | O(n log n)（排序） | O(n) |
