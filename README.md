# readyqueue225

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 多文件架构

实现刻意拆分为四个联动的实现文件，共享同一套语义：

- `readyqueue225/prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`，排序与回滚逻辑。
- `readyqueue225/validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一个 `validateBatch`，保证“先完整结构校验、再读取状态”。
- `readyqueue225/stats.go` — 线性一致的状态摘要 `Stats`（在同一把互斥锁内读取）。
- `readyqueue225/clone.go` — 深拷贝 `Clone`，复制逻辑时钟（now/generation/nextRevision）与全部条目，所有权完全独立。

## 索引与数据结构

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的 Enqueue 查重与 Cancel 定位。
- 弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序，不维护持久堆结构；队列规模受 `MaxItems` 约束，排序开销可控。

## 候选事务（Apply）

- 先做完整结构校验（kind 合法、ID 字符集与字节上限、Enqueue 的 ReadyAt 非负、Cancel 的 Priority/ReadyAt 必须为零、Now 非负），任何结构错误返回 `ErrInvalidInput` 且不读状态。
- 随后检查单调时间（`Now < now` 返回 `ErrTime`），再顺序执行 Enqueue/Cancel，Enqueue 分配递增 revision。
- 容量只在批次末尾检查；任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）通过逆序 undo 日志回滚条目、revision 时钟与逻辑时间，状态保持原子。
- 非空成功批次 generation 恰好加一；空批次不改变任何状态。

## 所有权与并发

- 所有公开方法用同一把 `sync.Mutex` 串行化，可安全并发调用；`Stats`/`Snapshot`/`Clone` 在锁内构造，保证线性一致。
- 返回的切片与 `Item` 均为按值拷贝，与内部状态隔离；`Clone` 重建 map，克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(k)，k 为批内操作数（末尾容量检查 O(1)）。
- `Pop`：O(m log m)，m 为就绪任务数（筛选 + 排序），删除 O(n)。
- `Snapshot`/`Clone`：O(m log m) / O(m)。
- `Stats`/`ValidateBatch`：O(1) / O(k·L)，L 为 ID 长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
