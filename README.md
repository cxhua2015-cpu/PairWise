# batchqueue

并发安全的内存型“批处理优先队列”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Item`：按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）、插入与删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选集惰性排序，保持写路径轻量。

## 候选事务

`Apply` 先在持锁状态下对整批 Op 做完整结构校验（kind、ID 字符集与字节上限、非负时间），再检查时间单调性，然后顺序执行 Enqueue/Cancel。执行期间维护一份 undo 日志（每个 Enqueue 记录新条目、每个 Cancel 记录被删条目）；任一操作失败或最终容量检查失败时，逆序回放日志恢复 map，并把 `nextRevision` 恢复到批次起点——时间、状态、revision 全部回滚，generation 不变。容量只在所有 Op 执行完毕后检查一次。非空成功批次 generation 恰好加一，空批次不变。

## 所有权

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）通过单一 `sync.Mutex` 串行化，可并发调用。
- `Pop` 与 `Snapshot` 返回的切片及其 `Item` 均为拷贝，调用方修改不会影响队列内部状态。

## 复杂度

设 n 为队列中条目数、k 为批次 Op 数、m 为就绪条目数：

- `Apply`：结构校验 O(k·L)（L 为 ID 长度），执行 O(k)，最终容量检查 O(1)；回滚（如发生）O(k)。
- `Pop`：扫描 O(n)，排序 O(m log m)，删除 O(min(m, limit))。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
