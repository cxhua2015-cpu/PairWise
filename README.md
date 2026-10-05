# reservationlease

并发安全的内存型“预留租约表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与所有权

- 主索引为 `map[string]Entry`，以键直接定位条目，无额外二级索引；条目按 `ExpiresAt <= Now` 惰性淘汰。
- `Table` 内所有可变状态（条目、单调时间 `now`、`generation`、`nextRevision`）由一把 `sync.Mutex` 保护，所有公开方法可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方持有所有权，修改不影响表内状态；条目按字典序排序以保证确定性输出。

## 候选事务（Apply）

1. 先对整个批次做结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、不超过 `MaxKeyBytes`、`Now >= 0`），不读取任何状态。
2. 加锁后检查单调时间（`Now < now` 返回 `ErrTime`）。
3. 在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. 最终容量超过 `MaxEntries` 或任一步出错（`ErrNotFound` 等）时整体回滚：淘汰、时间与 revision 均不生效。
5. 仅非空成功批次提交并使 `generation` 增加一次；空批次为无操作。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`）删除并返回条目，同时推进单调时间。

## 复杂度

- `Apply`：结构校验 O(批次数 × 键长)；候选复制 O(n)；执行 O(批次数)；整体 O(n + 批次数 × 键长)，n 为当前条目数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序输出）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。
