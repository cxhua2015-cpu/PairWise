# credentiallease

并发安全的内存型凭据租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位条目。
- 表级元数据（`now`、`generation`、`revision`）与索引同住一个 `Table` 结构，由一把 `sync.Mutex` 保护。
- `Snapshot` 与 `Expire` 的结果按键字典序排序，保证规范、可比较的顺序。

## 候选事务

`Apply` 采用候选事务（copy-on-write candidate）：

1. 先对整个批次做纯结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、键长不超限），不读取任何状态。
2. 加锁后检查时间单调性（`Now >= 0` 且不小于当前时间）。
3. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. 校验最终容量。任何一步失败（`ErrNotFound`/`ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚，真实状态毫发无损。
5. 全部成功才一次性提交候选，推进时间并将 generation 加一（空批次完全不变）。

`Expire` 使用相同的闭区间边界（`ExpiresAt <= now`），推进时间并返回被淘汰的条目。

## 所有权

- 所有公开方法可并发调用；互斥锁串行化写者，`Snapshot` 在锁内构造。
- 返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的独立副本，调用方修改不会影响内部状态。
- `Entry`/`Op`/`Batch` 等为纯值类型，跨 API 边界不发生共享引用。

## 复杂度

设 n 为当前条目数，b 为批次内操作数：

- `Apply`：结构校验 O(b·k)（k 为键长），候选复制 O(n)，执行 O(b)，总计 O(n + b·k)。
- `Expire`：O(n log n)（扫描 O(n)，结果按键排序 O(n log n)）。
- `Snapshot`：O(n log n)，同样因排序。
- 空间：O(n)，候选事务期间临时翻倍。
