# taskqueue105

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`（ID → 任务），Enqueue/Cancel/查重均为 O(1)。
- 不维护持久堆；Pop 与 Snapshot 时按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选集即时排序，保证顺序确定且实现简单。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做纯结构校验（时间非负、kind 合法、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态。
- 随后在互斥锁内把当前索引克隆为候选副本，按顺序在副本上执行 Enqueue（分配递增 revision）/Cancel；容量只在所有操作完成后对最终状态检查一次。
- 任一步失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，时间、状态与 revision 计数器天然回滚；成功时一次性提交，generation 恰好加一，空批次不产生任何变化。

### 所有权
- 所有公开方法由同一把 `sync.Mutex` 保护，可并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个操作，当前 n 个任务）：O(n + k)，克隆索引 O(n)，每个操作 O(1)。
- `Pop`（r 个就绪任务，取 m 个）：O(n + r log r)，弹出后原子删除。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
