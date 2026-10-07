# expirytable434

并发安全的内存型“到期状态表”，仅依赖标准库（Go 1.22+）。表使用显式非负单调时间：`Apply`/`Expire` 的 `Now` 不得小于当前逻辑时间，过期边界为闭区间 `ExpiresAt <= Now`。

## 架构

- `heartbeat.go` — 核心事务引擎：`Table`（`sync.Mutex` 保护）、`Apply`、`Expire`、`Snapshot` 与候选状态执行逻辑。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义：键为非空 ASCII 小写字母/数字/连字符/下划线且不超过 `MaxKeyBytes`，未知 kind、`Now < 0`、Put/Touch 的 `ExpiresAt <= Now` 均返回 `ErrInvalidInput`。
- `stats.go` — 线性一致的 `Stats` 摘要（同一把锁内读取）。
- `clone.go` — `Clone` 深拷贝，保留逻辑时钟（generation、nextRevision、now），所有权完全独立。
- `preview.go` — `Preview` 在一次线性化快照上复用完整事务语义，返回候选 `Result`/`Snapshot`/`Stats`；错误及优先级与同状态 `Apply` 一致，失败时全部返回零值，且不改变原对象任何状态。

## 索引

条目存储于 `map[string]Entry`（按键哈希索引），`Snapshot`/`Expire` 输出按字典序排序，保证确定性。除互斥锁外无额外索引结构。

## 候选事务

`Apply` 先完整结构校验再检查时间，然后在候选状态（当前条目的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配递增 revision。容量检查发生在全部操作之后（最终容量），任何错误（`ErrNotFound`、`ErrCapacity` 等）都会连同淘汰、时间和 revision 一起回滚——只有全部成功才提交。非空成功批次 generation 恰好加一，空批次不变。

## 所有权

所有返回的切片（`Snapshot.Entries`、`Expire` 结果）都是新分配的副本，与内部状态隔离；`Clone` 与 `Preview` 的候选结果同样不共享任何可变内存。调用方可自由修改返回值。

## 复杂度

设 n 为条目数、k 为批次操作数：

- `Apply` / `Preview`：O(n + k)（候选复制 + 淘汰扫描 + 顺序执行）。
- `Expire`：O(n + r log r)，r 为过期条目数（排序输出）。
- `Snapshot`：O(n log n)（排序）；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(k)，不读状态。
