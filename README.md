# resourcelease169

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

表内部以 `map[string]Entry` 作为主索引，键到租约条目为 O(1) 定位。
`Snapshot` 与 `Expire` 返回的条目按键字典序排序，保证输出确定性。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：在不读取任何状态的情况下校验整个批次（`Now` 非负、
   kind 合法、键为非空 ASCII 小写字母/数字/连字符/下划线且不超
   `MaxKeyBytes`、Put/Touch 的 `ExpiresAt` 非负），失败返回
   `ErrInvalidInput`。
2. **时间检查**：`Now` 小于当前时钟返回 `ErrTime`，时间非负且单调。
3. **候选执行**：在条目副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），
   再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
   最终条目数超过 `MaxEntries` 或任何步骤出错时，候选副本被直接丢弃，
   淘汰、时间与 revision 一并回滚。只有全部成功才提交，非空成功批次
   generation 恰好加一，空批次不改变任何状态。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）就地淘汰并推进
时钟，返回被移除的条目。

## 所有权

所有公开方法返回的切片（`Snapshot.Entries`、`Expire` 结果）都是新分配的
副本，调用方修改不会影响表内状态；表也不会保留调用方传入的切片。

## 并发与复杂度

全部公开方法由单一互斥锁保护，可安全并发调用。复杂度（n 为条目数，
m 为批次大小）：

- `Apply`：校验 O(m)，候选复制与淘汰 O(n)，执行 O(m)，整体 O(n + m)。
- `Expire`：O(n)。
- `Snapshot`：O(n log n)（含排序）。
- 单条 Put/Touch/Delete 摊还 O(1)（不计候选复制）。
