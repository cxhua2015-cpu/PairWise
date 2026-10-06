# readyqueue255

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`，持有互斥锁与状态。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共享同一套
  `validateBatchStruct`，保证预检与事务的结构语义完全一致（不读取、不修改队列状态）。
- `stats.go` — 线性一致统计：`Stats` 在同一把锁内读取，返回与并发事务一致的快照摘要。
- `clone.go` — 所有权安全的深拷贝：`Clone` 复制逻辑时钟（now/generation/nextRevision）
  与全部条目，克隆体与原队列完全隔离。

## 索引设计

- 主索引为 `map[string]Item`（按 ID），Enqueue/Cancel/去重均为 O(1)。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选集
  按需排序，不维护额外的有序结构，从而保持事务路径简单、回滚成本低。

## 候选事务（Apply）

1. 先做完整结构校验（与 `ValidateBatch` 共享），再读取状态。
2. 检查显式非负单调时间：`Batch.Now < now` 返回 `ErrTime`。
3. 顺序执行 Enqueue/Cancel，Enqueue 分配单调递增 revision，并记录 undo 日志。
4. 最终容量只在末尾检查；任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）
   通过 undo 日志回滚条目、revision 与时间，批次原子生效。
5. 非空成功批次 generation 恰好加一；空批次只推进时间，generation 不变。

## 所有权

- 所有公开方法在内部互斥锁下执行，支持并发调用。
- `Snapshot`/`Pop` 返回的切片均为新建拷贝，与内部状态隔离。
- `Clone` 深拷贝 map 与逻辑时钟，克隆体的后续变更不会泄漏到原队列。

## 复杂度

- `Apply`：O(k)，k 为批次内 op 数（不含排序）。
- `Pop`：O(n log n)，n 为当前就绪候选数（扫描 + 排序 + 删除）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`/`ValidateBatch`：O(1) / O(k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
