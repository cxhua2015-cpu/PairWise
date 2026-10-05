# resourcelease129

并发安全的内存型“资源租约表 129”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`（键 → 租约条目），Put/Touch/Delete/查找均为 O(1) 均摊。
- `Snapshot` 与 `Expire` 的结果按键名排序，保证输出确定性；返回的切片是新分配的副本，与内部状态完全隔离（调用方修改不影响表）。

### 候选事务
- `Apply` 先在持锁状态下对整个批次做**结构校验**（时间非负、kind 合法、键字符集与长度上限），再做**时间单调性检查**（`Now < 当前时间` 返回 `ErrTime`）。
- 随后在候选状态（条目的副本 map）上执行：先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配递增 revision。
- 最终容量超限（`ErrCapacity`）或任何错误（`ErrNotFound` 等）发生时直接丢弃候选状态，淘汰、时间与 revision 一并回滚，已提交状态不变。
- 全部成功后一次性提交：替换条目 map、推进时间与 revision 计数器；非空批次 generation 恰好 +1，空批次不变。

### 所有权与并发
- 所有公开方法（`Apply`/`Expire`/`Snapshot`）通过单把 `sync.Mutex` 串行化，支持任意并发调用；`-race` 下测试通过。
- 返回值（`Result`、`Snapshot`、淘汰条目切片）均为按值/新切片返回，表不保留对它们的引用，调用方独占所有权。

### 复杂度
- `Apply`：O(E + N)，E 为当前条目数（候选复制与淘汰扫描），N 为批内操作数。
- `Expire`：O(E + K log K)，K 为被淘汰条目数（排序）。
- `Snapshot`：O(E log E)（排序）；空间 O(E)。
