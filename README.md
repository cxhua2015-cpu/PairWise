# readyqueue355

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时按需物化切片并排序。队列容量受 `MaxItems` 上限约束，排序开销有界，换来实现的简洁与无堆修复路径。

### 候选事务（Apply）
- 先做整批结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），不读任何状态。
- 然后克隆主索引得到候选 map，在候选上顺序执行 Enqueue/Cancel：Enqueue 从候选 revision 计数器分配单调递增的 revision，Cancel 从候选删除。
- 容量检查只在最后进行（`len(candidate) > MaxItems` → `ErrCapacity`）。
- 任一步失败直接丢弃候选：时间、状态、revision 计数器全部天然回滚；成功才一次性提交（换 map、推进 `now`、`generation` 只增一次）。空批次不改变任何状态。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；`Item` 为纯值类型，无共享指针。
- 时间为显式非负单调值：`now < 0` → `ErrInvalidInput`，小于当前时间 → `ErrTime`，失败不推进时间。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个 op，n 个现存元素）：O(n + k)，克隆索引 O(n)，每个 op O(1)。
- `Pop`（r 个就绪元素）：O(n + r log r)，过滤 O(n)，排序 O(r log r)，删除 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
