# taskqueue185

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：队列内部使用 `map[string]Item` 作为唯一权威存储，按 ID O(1) 定位任务用于 Enqueue 去重与 Cancel 删除。Pop/Snapshot 时按需物化并排序，不维护常驻堆，避免双索引一致性开销。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（时间非负、kind 合法、ID 字符集与字节上限），再在队列状态的**拷贝**（候选 map 与候选 nextRevision）上顺序执行 Enqueue/Cancel；最终容量仅在全部操作执行完后检查一次。任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选状态，时间、状态、revision、generation 全部自然回滚；只有全部成功才一次性提交。非空成功批次 generation 恰好加一，空批次不变。
- **所有权**：所有公开方法通过单一 `sync.Mutex` 串行化，可并发调用。`Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不影响内部状态；`Item` 为值类型，无共享指针。
- **时间**：显式非负单调时间。`now < 0` 返回 `ErrInvalidInput`，`now < 当前时间` 返回 `ErrTime`；成功提交时推进队列时间。

## 复杂度

设 n 为队列中任务数，b 为批次操作数：

- `New`：O(1)
- `Apply`：O(n + b)（候选拷贝 + 顺序执行），容量检查 O(1)
- `Pop`：O(n log n)（物化排序后按 Priority 降序、ReadyAt 升序、ID 升序选取并原子删除）
- `Snapshot`：O(n log n)

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
