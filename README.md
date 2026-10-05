# deliveryqueue

并发安全的内存型“投递优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、插入与删除，用于 `Enqueue` 的 `ErrExists` 检查和 `Cancel` 的 `ErrNotFound` 检查。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选切片就地排序。队列规模受 `MaxItems` 上限约束，排序成本有界，换来写入路径的 O(1) 与实现的简洁可验证。

### 候选事务（Apply）
- `Apply` 先做**完整结构校验**（`Now >= 0`、kind 合法、ID 字符集与长度、`ReadyAt >= 0`），此时不读取任何状态；随后才在临界区内检查时间单调性（`Now >= now`，否则 `ErrTime`）。
- 批次在临界区内按序执行：每个操作记录一条 undo（插入记“此前不存在”，删除记“此前值”）。任一操作失败（`ErrExists`/`ErrNotFound`）或**末尾**最终容量检查失败（`ErrCapacity`）时，逆序回放 undo，并将 `nextRevision` 恢复到批次基线——时间、状态、revision 全部回滚，如同批次从未发生。
- 仅当非空批次整体成功时：`now` 前进到 `Batch.Now`，`generation` 恰好加一；空批次是完全的 no-op，不改变任何状态。

### 所有权与并发
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由一把 `sync.Mutex` 串行化，可安全并发调用；`Pop` 的选择与删除在同一临界区内原子完成。
- 返回值所有权完全移交调用方：`Pop` 与 `Snapshot` 返回的切片均为新分配的副本，内部 `Item` 按值复制，调用方修改返回值不影响队列状态，反之亦然。

### 复杂度
设 n 为队列中元素数，b 为批次中操作数：
- `New`：O(1)。
- `Apply`：结构校验 O(b·L)（L 为 ID 长度），执行 O(b)，末尾容量检查 O(1)；回滚 O(b)。
- `Pop`：O(n log n)（筛选就绪项 O(n) + 排序），删除 O(k)，k 为实际弹出数。
- `Snapshot`：O(n log n)，并额外 O(n) 空间用于隔离副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
