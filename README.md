# reorder

并发安全的内存多流序列重排器（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

- **索引**：每个 stream 一个 `streamState{next, buf}`，`buf` 为 `map[uint64][]byte` 哈希表，按序号 O(1) 定位未来事件；`next` 记录下一个待释放序号。全局用 `map[string]*streamState` 按流名索引。
- **连续释放**：事件序号等于 `next` 时立即释放并递增 `next`，随后循环检查 `buf[next]` 释放连续缓存前缀；`Skip` 在截断后同样释放连续后缀。释放输出按批次输入顺序排列，每条新连续链内按序号升序。
- **事务**：`PushBatch` 先对全部输入做结构校验（流名非空且不超长、序号 ∈ [1, MaxUint64-1]、Payload 非 nil），再在隔离的候选状态（注册表的深拷贝）上按输入顺序应用重复/冲突/过旧判断，最后才对最终候选状态检查流数、缓存事件数与 Payload 字节容量。任一步失败整体回滚且不返回 ready 事件；成功且状态有变化时 generation 只推进一次（幂等重复不推进）。
- **容量**：`Buffered` 只统计仍缓存的未来事件，`PayloadBytes` 为其字节长度之和；已释放事件不计入容量。容量超限返回 `ErrCapacity` 并回滚。
- **所有权**：接受的输入 Payload 在入库前深拷贝；返回的 ready 事件与 `Snapshot` 中的 Payload 均为独立深拷贝，调用方修改互不影响。
- **并发**：全部公开方法由单个 `sync.Mutex` 保护，可安全并发调用。

## 复杂度

- `PushBatch`：平均 O(batch + released)，外加候选状态克隆 O(streams + buffered)；释放链每事件摊还 O(1)。
- `Skip`：O(buffered) 扫描丢弃，加连续后缀释放。
- `Delete`：O(1)。
- `Snapshot`：O(S log S + B log B) 排序（S 为流数，B 为单流缓存事件数）。
- 空间：O(streams + buffered payload bytes)。
