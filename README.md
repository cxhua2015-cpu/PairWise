# readyqueue305

并发安全的内存型“就绪优先队列 305”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 的删除。
- 不维护持久堆：Pop/Snapshot 时把 `ReadyAt <= now` 的候选收集到切片后按
  （Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，
  排序成本可控，且避免堆与 map 双索引的一致性负担。

### 候选事务（Apply）
- Apply 在单把互斥锁内执行：先做整批结构校验（Now 非负、kind 合法、ID 字符集与
  字节上限、ReadyAt 非负），再做单调时间检查（`ErrTime`），然后在**克隆的 map 与
  revision 计数器副本**上顺序执行 Enqueue/Cancel，最后才检查容量。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，时间、
  状态、revision 天然回滚；全部成功才一次性提交。非空成功批次 generation 恰好 +1，
  空批次只推进时间、不改 generation。

### 所有权
- 所有公开方法由一把 `sync.Mutex` 保护，可并发调用。
- `Item` 为纯值类型；`Pop` 与 `Snapshot` 返回的切片均为新分配的副本，
  调用方修改不影响内部状态，内部状态也不别名返回的切片。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个 op、当前 n 项）：O(n + k)，克隆 map 为 O(n)，容量检查 O(1)。
- `Pop`（r 个就绪项）：O(n + r log r)。
- `Snapshot`：O(n log n)（按规范顺序排序后返回）。
