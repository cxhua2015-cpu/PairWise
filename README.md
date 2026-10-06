# expirytable209

并发安全的内存型“到期状态表 209”，仅依赖 Go 1.22+ 标准库。语义详见 `SPEC.md`。

## 索引

- 主存储为 `map[string]Entry`，按键 O(1) 定位。
- 另维护一个按 `ExpiresAt` 排序的最小堆作为到期索引。堆项采用惰性失效：
  Put/Touch 覆盖旧值时只追加新堆项，弹出时若与主存储中的 `ExpiresAt`
  不一致则视为陈旧项丢弃，避免堆内删除的额外开销。

## 候选事务

`Apply` 先在候选状态（主存储与堆的副本）上执行：淘汰 `ExpiresAt <= Now`
的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision），最后
做容量检查。任一步失败（`ErrNotFound`/`ErrCapacity` 等）直接丢弃候选，
淘汰、时间与 revision 一并回滚，已提交状态不受任何影响。非空成功批次
generation 恰好加一；空批次校验通过后不改变任何状态。

## 所有权与并发

- 所有公开方法由同一把互斥锁保护，可安全并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为独立副本，调用方修改不会影响
  内部状态；内部也从不保留调用方传入的切片。
- 批次先做完整结构校验（kind、键字符集与长度、非负时间），再做单调
  时间检查，最后才读取状态。

## 复杂度

- `Apply`：O(B + E·log N + P·log N)，B 为批内操作数，E 为淘汰条数，
  P 为 Put/Touch 次数，N 为堆大小（含候选复制 O(N)）。
- `Expire`：O(K·log N)，K 为实际到期条数；惰性清理陈旧堆项摊还 O(log N)。
- `Snapshot`：O(N log N)（复制后按键排序，保证输出顺序确定）。
