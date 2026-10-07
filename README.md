# readyqueue330

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，而是在 `Pop`/`Snapshot` 时对当前条目一次性排序，保持实现简单且行为确定。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind、ID 字符集与字节上限、非负时间），不读取任何状态。
- 通过校验后在单个互斥锁内，把全部操作应用到一份 map 副本上：Enqueue 分配递增 revision，Cancel 删除条目；任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时直接丢弃副本，时间、状态与 revision 计数器天然回滚。
- 仅在全部成功时一次性提交副本、推进单调时间并将 generation 加一；空批次为无操作，generation 不变。

**所有权与并发**
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可安全并发调用。
- 返回的切片（`Pop` 结果、`Snapshot.Items`）均为新分配的副本，调用方修改不会影响内部状态。
- `Pop` 在锁内按规范顺序选择 `ReadyAt <= now` 的条目并原子删除；`now` 为负或 `limit <= 0` 返回 `ErrInvalidInput`，`now` 小于当前时间返回 `ErrTime`。

**复杂度**（n = 队列中条目数，k = 批次操作数，m = 弹出上限）
- `Apply`：结构校验 O(k)，候选副本与提交 O(n + k)。
- `Pop`：排序 O(n log n)，选择 O(n)，删除 O(m)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
