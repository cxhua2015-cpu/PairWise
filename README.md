# taskqueue185

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队、取消与删除。
- 不维护有序堆：`Pop`/`Snapshot` 时对候选集按（Priority 降序、ReadyAt 升序、ID 升序）排序。`MaxItems` 是队列的硬上限，按需排序在受限规模下更简单且无堆修复开销。

**候选事务（Apply）**
- `Apply` 先在持锁状态下做整批结构校验（时间非负且不回退、kind 合法、ID 字符集与字节上限），再顺序执行 Enqueue/Cancel。
- 执行期间记录 undo 日志（新插入记“删除”、被取消的记“恢复原值”）；任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时逆序回放 undo 并恢复 `nextRev`，时间、状态、revision 全部回滚，批次原子生效。
- 非空成功批次 generation 恰好 +1；空批次不改变 generation。revision 由单调递增的 `nextRev` 分配，从 1 开始。

**所有权与并发**
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；`Apply`/`Pop` 的原子性由互斥锁保证。
- 时间是显式非负单调时钟：`Apply` 与 `Pop` 都校验 `now >= 当前时间`（否则 `ErrTime`），成功后推进时钟；失败不推进。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不会影响内部状态。

**复杂度**（n = 当前任务数，b = 批次大小）
- `New`：O(1)。
- `Apply`：O(b) 校验与执行，回滚 O(b)。
- `Pop`：O(n) 扫描就绪候选 + O(n log n) 排序，删除 O(min(n, limit))。
- `Snapshot`：O(n log n)，返回排序后的副本。
- 空间：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
