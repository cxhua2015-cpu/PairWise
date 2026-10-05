# auctionqueue

并发安全的内存型“竞价优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于存在性判断与 Cancel。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久堆，而是在 `Pop`/`Snapshot` 时对候选集即时排序，保证实现简单且顺序严格确定。

### 候选事务
- `Apply` 先在暂存计划（staged plan）上顺序执行整个批次：结构校验全部通过后才读取状态；任一 op 失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）即丢弃暂存区，时间、状态与 revision 计数器天然回滚，无需补偿日志。
- 容量只在批次末尾检查一次，允许“中途超限、末尾回落”的批次成功。
- 全部成功后一次性提交：应用暂存变更、推进单调时间、更新 `NextRevision`，非空批次 `Generation` 恰好加一。

### 所有权
- 队列内部不暴露任何可写引用：`Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用。

### 复杂度
- `Apply`：O(k)，k 为批次内 op 数（校验、暂存、提交均为线性）。
- `Pop`：O(n log n)，n 为当前就绪任务数（筛选 + 排序 + 删除）。
- `Snapshot`：O(n log n)，全量复制并排序。
- 空间：O(n)，n 为队列中任务数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
