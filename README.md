# readyqueue245

并发安全的内存型“就绪优先队列 245”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 多文件架构

- `readyqueue245/prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`，规范排序与候选事务。
- `readyqueue245/validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构校验（`validateBatch`），只读 Options，不触碰队列状态。
- `readyqueue245/stats.go` — 线性一致统计：`Stats` 在同一把互斥锁内读取逻辑时钟与条目数。
- `readyqueue245/clone.go` — 深拷贝：`Clone` 复制全部逻辑时钟（now/generation/nextRevision）与条目，所有权完全隔离。

## 索引与数据结构

- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel（`ErrNotFound`）。
- 弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）通过 `less` 比较器在取出时排序实现；`Snapshot` 返回同一规范顺序的副本。

## 候选事务与回滚

`Apply` 先做完整结构校验（与 `ValidateBatch` 相同），再在锁内把全部 Op 顺序应用到一份 staged map 上：Enqueue 从候选 `nextRevision` 起分配 revision，Cancel 删除条目，最终容量只在末尾检查（`ErrCapacity`）。任一失败直接丢弃候选，时间、状态、revision、generation 全部不变；成功才一次性提交，非空批次 generation 恰好加一，空批次不变。

## 所有权与并发

- 所有公开方法持有同一把 `sync.Mutex`，可任意并发调用；`Stats`/`Snapshot`/`Clone` 与并发事务线性一致。
- `Snapshot` 返回的切片、`Clone` 返回的队列均为独立副本，调用方修改不会影响内部状态。

## 复杂度

- `Apply`：O(n + k)，n 为现有条目数（拷贝候选），k 为批次 Op 数。
- `Pop`：O(n + m log m)，m 为就绪候选数；原子删除选中的至多 limit 条。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
