# readyqueue380

并发安全的内存型“就绪优先队列”，语义见 `SPEC.md`。Go 1.22+，仅标准库。

## 设计说明

### 索引

队列内部以 `map[string]Item` 作为主索引，键为任务 ID，值内联存储
`Priority`、`ReadyAt`、`Revision`。Enqueue/Cancel/去重判断均为 O(1) 均摊。
不维护额外的有序结构：就绪选择（`Priority` 降序、`ReadyAt` 升序、`ID` 升序）
在 `Pop`/`Snapshot` 时现算，避免在写路径上维护堆或树。

### 候选事务（Apply）

`Apply` 是一个全有或全无的候选事务：

1. **结构校验**：在读取任何状态之前，完整校验 `Now >= 0`、每个 Op 的
   `Kind` 合法、ID 非空且只含 `[a-z0-9-_]` 并不超过 `MaxIDBytes`；
   失败返回 `ErrInvalidInput`。
2. **单调时间**：`Now < 当前时间` 返回 `ErrTime`；空批次直接成功返回，
   不改变时间、状态与 generation。
3. **顺序执行**：Enqueue 检查重复（`ErrExists`）并分配递增 revision；
   Cancel 检查存在（`ErrNotFound`）。期间记录 undo 日志。
4. **末尾容量**：只在所有 Op 执行完后检查最终大小是否超过 `MaxItems`，
   超出返回 `ErrCapacity`。
5. **提交或回滚**：任一步失败都按 undo 日志逆序回滚条目、revision 计数器
   与时间，队列保持进入批次前的状态；成功则推进时间，非空批次
   generation 恰好加一。

### 所有权与并发

所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把互斥锁串行化，可安全并发
调用。`Pop` 的选择与删除是原子的。`Snapshot` 与 `Pop` 返回的切片均为新
分配的副本，调用方修改不会影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度

设 n 为队列大小，b 为批次 Op 数，k 为就绪候选数，m 为弹出上限：

- `New`：O(1)
- `Apply`：O(b)（校验 + 哈希表读写 + undo 日志）
- `Pop`：O(n + k log k)，扫描就绪候选后排序，删除 O(m)
- `Snapshot`：O(n log n)，复制并排序全部条目
- 空间：O(n)
