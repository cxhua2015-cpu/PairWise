# notificationqueue

并发安全的内存型通知优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性检查、入队与取消。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需物化排序，
  不维护持久堆结构；队列规模受 `MaxItems` 上限约束，排序开销可控。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），再读取任何状态。
- 随后在克隆的候选索引上顺序执行 Enqueue/Cancel：Enqueue 分配单调递增的 revision，
  Cancel 删除对应项；容量检查只在批次末尾进行。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选索引，
  时间、状态、revision 与 generation 全部回滚；成功才一次性提交，
  非空批次 generation 恰好加一，空批次不变（时间仍前进）。

### 所有权
- 所有公开方法通过一把 `sync.Mutex` 串行化，支持并发调用。
- `Pop` 返回的切片与 `Snapshot().Items` 均为新分配的副本，调用方修改不会影响内部状态。
- `Pop` 在选择的同时原子删除所选项；时间（`Batch.Now`/`Pop` 的 now）必须非负且单调，
  倒退返回 `ErrTime`，负数返回 `ErrInvalidInput`。

### 复杂度
- `New`：O(1)。
- `Apply`：O(n + k)，n 为当前队列大小（克隆索引），k 为批内操作数。
- `Pop`：O(n log n)，n 为就绪项数量（过滤 + 排序 + 删除）。
- `Snapshot`：O(n log n)（拷贝 + 排序）。
- 空间：O(n)，n ≤ `MaxItems`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
