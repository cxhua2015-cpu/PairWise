# expirytable304

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位，覆盖 Put/Touch/Delete 与存在性检查。
- 未维护堆等按时间排序的二级索引；到期扫描为全表遍历（见“复杂度”）。
- `Snapshot` 与 `Expire` 返回的条目按键名排序，保证确定性输出。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、键字符集与长度、`ExpiresAt` 非负），再检查时间单调性。
2. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配修订号。
3. 全部成功后做最终容量检查；任何错误（含 `ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚，原状态不变。
4. 成功时一次性提交候选，`Now` 前进，非空批次 generation 恰好加一，空批次不改变任何状态。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`）就地淘汰并推进时间。

## 所有权

- 所有公开方法由单一互斥锁保护，可并发调用。
- 返回的 `[]Entry` 与 `Snapshot` 均为独立副本，调用方修改不影响表内状态；表也不会保留调用方传入的切片。

## 复杂度

- `Apply`：O(n + m)，n 为批内操作数，m 为当前条目数（候选复制与淘汰扫描）。
- `Expire`：O(m + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(m log m)（拷贝并排序）。
- 空间：O(m)。
