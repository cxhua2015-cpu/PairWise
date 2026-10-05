# resourcelease174

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位，键即唯一身份。
- `Snapshot` 与 `Expire` 返回的条目按键名字典序排序，保证输出确定性；返回切片均为新建副本，与内部状态完全隔离（调用方修改不影响表）。

## 候选事务

`Apply` 分三阶段，且任意失败都整体回滚：

1. **结构校验**：先完整校验 `Now >= 0` 与全部 Op（kind 合法、键非空且仅含 `[a-z0-9-_]`、不超过 `MaxKeyBytes`、`ExpiresAt >= 0`），不读取任何状态，因此结构错误优先于时间错误。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`；空批次在此之后直接成功返回，不改变任何状态（generation 不变）。
3. **候选执行**：在条目副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put（upsert）/ Touch（须存在）/ Delete（须存在）；Put/Touch 各分配一个递增 revision。最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。任一错误发生时副本被丢弃，淘汰、时间与 revision 一并回滚；只有全部成功才一次性提交，generation 恰好加一。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）与单调时间约束，返回被移除的条目。

## 所有权与并发

- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用；批次之间线性化。
- 时间显式非负且单调：`Apply`/`Expire` 只会把时钟向前推进。
- 表不保留调用方传入的切片，返回值也不暴露内部引用。

## 复杂度

- `Apply`：O(n + e)，n 为批次内 op 数，e 为当前条目数（候选复制与淘汰扫描）。
- `Expire`：O(e + k log k)，k 为移除条目数（排序）。
- `Snapshot`：O(e log e)（复制并排序）。
- 空间：O(e)。
