# taskqueue095

并发安全的内存型任务优先队列，实现见 `taskqueue095/prioritybox.go`，语义以 `SPEC.md` 与契约测试为准。

## 索引

- 主存储为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists` / `ErrNotFound`）。
- 不维护持久堆；`Pop` 与 `Snapshot` 在持有锁时按需对候选集排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。

## 候选事务

- `Apply` 先做整批结构校验（kind、ID 字符集与字节上限），不读取任何状态。
- 随后在锁内把当前 map 复制为候选副本，在副本上顺序执行 Enqueue/Cancel 并递增 revision，最后才检查容量上限。
- 任一步失败直接丢弃候选副本，已提交的时间、状态与 revision 计数器完全不变（天然回滚）；成功则整体替换并提交，非空批次 generation 恰好加一。

## 所有权

- 所有公开方法通过单一 `sync.Mutex` 串行化，支持并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改返回值不影响内部状态。

## 复杂度

- `Apply`：O(n + k)，n 为当前任务数（候选复制），k 为批内操作数。
- `Pop`：O(n log n)，n 为就绪任务数；删除为 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
