# devicelease

并发安全的内存型设备租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引：`map[string]Entry`，按键 O(1) 定位，用于 Put/Touch/Delete 与容量计数。
- 过期扫描：未维护额外堆索引；Apply/Expire 时全量扫描并按 `ExpiresAt <= Now`（闭区间）淘汰，实现简单且无并发堆修复成本。
- `Snapshot`/`Expire` 返回的条目按 Key 排序，保证确定性输出。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做结构校验（kind 合法、键字符集/长度、非负时间），再做单调时间检查（`Now < 当前时间` 返回 `ErrTime`），全程不读改状态。
2. **候选执行**：在条目副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision。最终容量超限或任何错误（`ErrNotFound`/`ErrCapacity`）直接丢弃候选——淘汰、时间和 revision 一并回滚，已提交状态零副作用。

只有全部成功才一次性提交：替换条目表、推进时间、推进 revision 计数器；非空批次 generation 恰好 +1，空批次只推进时间与淘汰，generation 不变。

## 所有权

- 所有公开方法由单一 `sync.Mutex` 保护，可任意并发调用。
- `Snapshot`/`Expire` 返回的切片与 `Entry` 均为拷贝，调用方修改不影响内部状态；表不保留调用方传入的切片。
- `Table` 内含互斥锁，复制 `Table` 值是错误用法，请始终使用 `New` 返回的指针。

## 复杂度

设 n 为当前条目数、m 为批次操作数：

- `Apply`：时间 O(n + m)，空间 O(n)（候选副本）。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝 + 排序）。
- `New`：O(1)。
