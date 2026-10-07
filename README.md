# expirytable304

并发安全的内存型“到期状态表 304”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → 条目），Put/Touch/Delete/查找均为 O(1) 均摊。
- `Snapshot` 与 `Expire` 的结果按键名字典序排序，保证输出确定性；返回的切片为新分配，与内部状态完全隔离，调用方可自由修改。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind 合法、键非空且仅含 `[a-z0-9_-]`、键长 ≤ `MaxKeyBytes`），再检查时间单调性（`Now >= 0` 且不小于当前时间，否则 `ErrTime`）。
2. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 目标不存在返回 `ErrNotFound`。
3. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。
4. 任何错误都整体回滚：淘汰、时间、revision、generation 均不变；只有全部成功才一次性提交。非空成功批次 generation 恰好加一，空批次不变。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）删除并返回被淘汰条目，同样受单调时间约束。

## 所有权与并发

- 所有公开方法通过单个 `sync.Mutex` 串行化，可安全并发调用。
- 表拥有内部全部状态；`Snapshot`/`Expire` 返回的切片与 `Entry` 值均为副本，不与内部共享内存。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（排序）；空间 O(n)。
