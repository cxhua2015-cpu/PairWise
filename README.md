# expirytable394

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

- 内部使用单一 `map[string]Entry` 作为主索引，键即条目键，查找/更新均为 O(1)。
- `Snapshot`/`Expire` 返回的条目按键排序，保证输出确定性。
- 一把 `sync.Mutex` 保护全部状态（索引、`now`、`generation`、`nextRevision`），所有公开方法可并发调用。

## 候选事务（candidate transaction）

`Apply` 按以下顺序执行：

1. **结构校验**：先完整校验整个批次（kind 合法、键非空且仅含 `[a-z0-9-_]`、键长 ≤ `MaxKeyBytes`、`Now >= 0`），失败返回 `ErrInvalidInput`，不读取任何状态。
2. **时间检查**：`Now` 必须非负且不小于当前表时间，回退返回 `ErrTime`。
3. **候选状态**：克隆索引，在克隆上先删除 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 目标不存在返回 `ErrNotFound`。
4. **最终容量检查**：候选条目数超过 `MaxEntries` 返回 `ErrCapacity`。
5. **提交**：仅当全部成功才换入候选索引、推进时间与 revision 计数，非空批次 `generation` 恰好加一；空批次不改变任何状态。

任何错误都发生在提交之前，因此淘汰、时间和 revision 随候选一起整体回滚。`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）并推进表时间。

## 所有权

- 表不保留调用方传入的切片；`Snapshot().Entries` 与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改，不影响内部状态。
- `Entry`/`Result`/`Snapshot` 均为值类型，按值返回。

## 复杂度

- `Apply`：O(n + e)，n 为批次 op 数，e 为当前条目数（克隆索引）。
- `Expire`：O(e + k log k)，k 为被逐出条目数（排序）。
- `Snapshot`：O(e log e)（排序）。
- 空间：O(e)。
