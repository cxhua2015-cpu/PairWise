# expirytable349

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，键即条目 Key，Put/Touch/Delete 与过期扫描均为哈希定位。
- 单把 `sync.Mutex` 保护全部内部状态（`now`、`generation`、`nextRevision`、条目表），所有公开方法可直接并发调用。
- `Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

## 候选事务（Apply）

1. **结构校验**：先完整校验整批 Op（kind 合法、键非空且仅含 `[a-z0-9-_]`、字节长度不超限），再读取任何状态；失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须非负且不早于当前表时间，否则 `ErrTime`。
3. **候选状态**：在条目表的副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 目标缺失返回 `ErrNotFound`。
4. **提交或回滚**：最终条目数超过 `MaxEntries` 返回 `ErrCapacity`；任何错误都会连同淘汰、时间与 revision 一起回滚——只有全部成功才用候选状态替换正式状态。非空成功批次 `generation` 恰好加一，空批次不变。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）移除并返回条目，并推进表时间。

## 所有权

- 表不保留调用方传入的切片；`Snapshot.Entries` 与 `Expire` 的返回值都是新分配的副本，调用方可自由修改，不影响内部状态。
- `Entry`/`Result`/`Snapshot` 均为值类型，按值返回，无共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰扫描），m 为批次内 Op 数；每个 Op 为 O(1)。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序）；空间上每次调用 O(n)。
