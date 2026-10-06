# expirytable209

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。

## 索引与所有权

- 内部唯一索引为 `map[string]Entry`，以键为唯一标识；`Put` 为 upsert，`Touch`/`Delete` 要求键存在，否则 `ErrNotFound`。
- 表独占其内部状态；`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响表内状态。
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；`Apply`/`Expire`/`Snapshot` 之间完全串行化。

## 候选事务（Apply）

1. 先对整个批次做结构校验（`Now >= 0`、kind 合法、键非空且仅含 `[a-z0-9-_]` 且不超过 `MaxKeyBytes` 字节、`Put`/`Touch` 的 `ExpiresAt >= 0`），再检查时间单调性（`Now < 当前时间` 返回 `ErrTime`）。
2. 在候选状态（当前条目的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再按顺序执行 `Put`/`Touch`/`Delete`；`Put`/`Touch` 各分配一个单调递增的 revision。
3. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`；任何错误都会整体回滚——淘汰、时间与 revision 均不生效（候选状态直接丢弃）。
4. 成功时提交候选状态并推进时间；非空批次 generation 恰好加一，空批次 generation 不变（时间仍推进）。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`）删除并返回条目，同样校验非负与单调时间。

## 复杂度

- `Apply`：O(E + K)，E 为当前条目数（候选复制与淘汰扫描），K 为批次内操作数。
- `Expire`：O(E + R log R)，R 为本次删除的条目数（按键排序返回）。
- `Snapshot`：O(E log E)（返回按键排序的副本）。
- 空间：O(E)。
