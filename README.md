# tokenvault

并发安全的内存型令牌到期库（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry` 哈希表，键查找 / 插入 / 删除均为 O(1)。
- 到期不维护额外堆索引：`Apply` 与 `Expire` 通过一次全量扫描删除 `ExpiresAt <= Now`（闭区间）的条目，O(n)。
- `Snapshot` 将条目按 Key 字典序排序后返回，O(n log n)，保证规范顺序稳定。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做结构校验（未知 kind、非法键返回 `ErrInvalidInput`），再检查时间（`Now` 为负或小于当前时间返回 `ErrTime`）。
2. **候选状态**：在条目表的副本上先淘汰 `ExpiresAt <= Now` 的条目，再按序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。Touch/Delete 目标不存在返回 `ErrNotFound`，最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都会整体回滚——淘汰、时间与 revision 分配一并作废，表保持批次前的状态。非空成功批次 generation 恰好加一；空批次只推进时间，generation 不变。

## 所有权

- 所有公开方法持有同一把互斥锁，可任意并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响内部状态；`Entry` 为纯值类型，无共享指针。
- 表不保留调用方传入的 `Batch`/`Op` 引用，键与值在写入时被拷贝进内部条目。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `New` | O(1) | O(1) |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)，k 为到期数 | O(k) |
| `Snapshot` | O(n log n) | O(n) |
