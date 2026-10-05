# resourcelease104

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，按键 O(1) 定位；`Snapshot`/`Expire` 返回的条目按键名排序，保证确定性输出。
- 表内维护单调时钟 `now`、批次计数 `generation` 与下一个可分配的 `nextRevision`（从 1 开始）。

## 候选事务（Apply）

`Apply` 采用候选状态事务：

1. 先对整个批次做结构校验（kind 合法、键为非空 ASCII `[a-z0-9-_]` 且不超过 `MaxKeyBytes`），再检查时间（`Now` 非负且不早于当前时间），空批次为无操作。
2. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision，Touch/Delete 目标不存在返回 `ErrNotFound`。
3. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。任何错误都直接丢弃候选副本，淘汰、时间与 revision 一并回滚，已提交状态不受影响。
4. 非空成功批次提交后 `generation` 恰好加一。

`Expire(now)` 使用相同的闭区间边界删除到期条目并推进时间；有条目被移除时 `generation` 加一。

## 所有权与并发

- 所有公开方法由单一互斥锁保护，可任意并发调用。
- 返回值（`Snapshot.Entries`、`Expire` 结果）均为新分配的切片与条目副本，调用方修改不会影响内部状态。

## 复杂度

- `Apply`：O(n + k)，n 为当前条目数（候选复制与淘汰扫描），k 为批内操作数；单操作均摊 O(1)。
- `Expire`：O(n + m log m)，m 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序输出）；空间 O(n)。
