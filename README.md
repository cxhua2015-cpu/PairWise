# resourcelease094

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

`Table` 内部以 `map[string]Entry` 作为唯一主索引，键即租约名称。`Snapshot` 与
`Expire` 返回的条目按键排序，且均为拷贝，与内部状态完全隔离。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：完整校验批次中所有 op 的 kind、键字符集与长度、`ExpiresAt`，
   任一不合法即返回 `ErrInvalidInput`，不读取任何状态。
2. **时间检查**：`Now` 必须非负且不早于表当前时间，否则返回 `ErrTime`。
3. **候选执行**：在条目副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行
   Put/Touch/Delete；Put/Touch 各自分配一个递增 revision。Touch/Delete 目标
   不存在返回 `ErrNotFound`；最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。
   任何错误都整体回滚——淘汰、时间与 revision 均不生效。非空成功批次
   generation 恰好加一；空批次不改变任何状态。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）淘汰并返回被移除条目。

## 所有权

所有公开方法持有同一把互斥锁，可安全并发调用。`Snapshot`/`Expire` 返回的切片
为新建拷贝，调用方修改不影响表；表也不会保留对返回切片的引用。

## 复杂度

- `Apply`：O(n + e)，n 为批次 op 数，e 为当前条目数（候选副本与淘汰扫描）。
- `Expire`：O(e + k log k)，k 为被移除条目数（排序）。
- `Snapshot`：O(e log e)（拷贝并排序）。
- 空间：O(e)。
