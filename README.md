# readyqueue205

并发安全的内存型“就绪优先队列”。详见 `SPEC.md`。Go 1.22+，仅标准库。

## 设计说明

- **索引**：队列主体为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel（`ErrNotFound`）。不维护堆；Pop/Snapshot 时按需对候选切片排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。
- **候选事务**：`Apply` 先做完整结构校验（Now 非负、kind 合法、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态；随后在单个互斥锁临界区内顺序执行 Enqueue/Cancel，并记录撤销日志（undo log）。任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，按逆序回放撤销日志，时间、generation 与 revision 计数器保持原值，实现原子回滚。容量只在批次末尾检查，因此“先 Cancel 再 Enqueue”的替换型批次在满队列上也能成功。
- **所有权**：`Queue` 内部状态（`now`、`generation`、`nextRevision`、`items`）仅由持有 `sync.Mutex` 的临界区访问，所有公开方法（`Apply`/`Pop`/`Snapshot`）均可并发调用。`Pop` 返回的切片与 `Snapshot().Items` 均为新分配的副本，调用方修改不会影响内部状态；返回的 `Item` 为值拷贝。
- **时间**：显式非负单调时间。`Apply.Now` 与 `Pop` 的 `now` 必须 ≥ 0（否则 `ErrInvalidInput`）且不早于队列当前时间（否则 `ErrTime`）；成功后队列时间推进到该值。非空成功批次 generation 恰好 +1，空批次不变；revision 从 1 开始，仅由成功的 Enqueue 分配。
- **复杂度**：`Apply` 为 O(k)，k 为批内 op 数（回滚同为 O(k)）；`Pop` 为 O(n log n)，n 为当前就绪任务数；`Snapshot` 为 O(m log m)，m 为队列长度；`New` 为 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
