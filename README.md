# resourcelease159

并发安全的内存型资源租约表（见 `SPEC.md`）。表使用显式非负单调时间，
`Apply` 以候选事务方式执行，`Expire`/`Snapshot` 与内部状态隔离。

## 索引

条目存储在 `map[string]Entry` 哈希索引中，按键 O(1) 定位。
`Snapshot` 与 `Expire` 返回的切片按键排序，保证输出确定性。

## 候选事务

`Apply` 先做整批结构校验（kind、键字符集与字节上限、非负时间），
再检查时间单调性；随后在候选状态（条目的独立副本）上先淘汰
`ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch
分配递增 revision。最终容量超限或任何错误都会丢弃候选，淘汰、
时间与 revision 一并回滚；只有非空成功批次才使 generation 加一。

## 所有权

所有公开方法通过互斥锁串行化，可并发调用。`Snapshot` 与 `Expire`
返回的切片均为新分配的副本，调用方修改不会影响表内状态。

## 复杂度

- `Apply`：O(n + m)，n 为存活条目数（候选复制与淘汰），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。
