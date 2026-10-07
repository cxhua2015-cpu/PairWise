# readyqueue395

并发安全的内存型“就绪优先队列”。语义详见 `SPEC.md`。Go 1.22+，仅标准库。

## 索引与所有权

- 队列内部只持有一份 `map[string]Item`：以任务 ID 为键的所有权索引，`Item` 为值类型，map 即唯一权威存储。
- 不维护独立的堆/有序索引；`Pop` 与 `Snapshot` 在持锁期间从 map 收集候选并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，避免多索引不一致。
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用；`New` 之外无锁外共享状态。
- 返回的切片（`Pop`、`Snapshot.Items`）均为新分配的拷贝，调用方修改不影响内部状态。

## 候选事务（Apply）

`Apply` 是一个全有或全无的候选事务：

1. **结构校验**：先完整校验整个批次（`Now >= 0`、kind 合法、ID 字符集与字节上限、`ReadyAt >= 0`），失败返回 `ErrInvalidInput`，不读取任何状态。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`。
3. **顺序执行**：按序执行 Enqueue/Cancel。Enqueue 分配单调递增的 revision（从 1 开始）；重复 ID 返回 `ErrExists`，Cancel 缺失 ID 返回 `ErrNotFound`。
4. **末尾容量检查**：所有操作执行完后才检查 `MaxItems`，超限返回 `ErrCapacity`（因此同批次内“先取消再入队”可以成功）。
5. **回滚**：任一失败通过逆序 undo 日志恢复 map，并回收已分配的 revision；时间与 generation 保持不变。
6. **提交**：非空成功批次 generation 恰好加一并推进时间；空批次不改变时间与 generation。

`Pop(now, limit)` 同样推进单调时间（`now < 0` 或 `limit < 1` 返回 `ErrInvalidInput`，`now` 倒退返回 `ErrTime`），在 `ReadyAt <= now` 的候选中按规范顺序取前 `limit` 个并原子删除。

## 复杂度

设 n 为队列中的任务数，b 为批次数，k 为弹出的候选数：

- `New`：O(1)。
- `Apply`：O(b) 校验与执行，O(b) 回滚（仅失败时）；map 操作均摊 O(1)。
- `Pop`：O(n) 扫描就绪候选 + O(k log k) 排序 + O(k) 删除。
- `Snapshot`：O(n log n) 排序，返回独立拷贝。
- 空间：O(n)。
