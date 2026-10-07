# readyqueue315

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计

### 索引
- **主索引**：`map[string]Item`，以 ID 为键，Enqueue/Cancel 的去重与查找均为 O(1)。
- **规范顺序**：不维护持久有序结构；`Pop`/`Snapshot` 时按需物化并按规范顺序排序——Priority 降序、ReadyAt 升序、ID 升序。`Pop` 先以 `ReadyAt <= now` 过滤候选，再排序取前 `limit` 个。

### 候选事务
`Apply` 先对整个批次做完整结构校验（`Now >= 0`、kind 合法、ID 字符集与字节上限），再在互斥锁内把时间戳与当前时间比较。随后把主索引**复制为候选 map**，在候选上顺序执行 Enqueue（分配递增 revision）/Cancel，最后才做容量检查。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选——时间、状态、revision 计数器全部天然回滚；成功才一次性提交候选 map、revision 与 `now`，非空批次 generation 恰好加一。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用；锁内无阻塞操作。
- 返回的切片（`Pop`、`Snapshot.Items`）都是新分配的副本，`Item` 为值类型，调用方修改返回值不影响内部状态。
- 时间为显式非负单调值：`now < 0` 或 `limit <= 0` 返回 `ErrInvalidInput`；`now` 小于队列当前时间返回 `ErrTime`；成功的 `Apply`/`Pop` 将队列时间推进到 `now`。

### 复杂度
设 n 为队列中元素数，b 为批次大小：
- `New`：O(1)。
- `Apply`：O(n + b)——候选 map 复制 O(n)，b 个操作各 O(1)。
- `Pop`：O(n log n)——扫描过滤 O(n)，排序 O(n log n)，删除 O(limit)。
- `Snapshot`：O(n log n)，排序输出。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
