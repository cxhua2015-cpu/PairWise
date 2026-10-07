# readyqueue390

并发安全的内存型“就绪优先队列”，实现见 `readyqueue390/prioritybox.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

- **索引**：队列内部使用 `map[string]Item` 以 ID 为键做 O(1) 查找、入队与取消；`Pop`/`Snapshot` 时把命中项拷贝到切片切片后按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。
- **候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、时间非负），再在互斥锁内顺序执行 Enqueue/Cancel；每个操作记录一条 undo 日志，任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时逆序回滚全部状态、时间与 revision，保证批次原子性。非空成功批次 generation 恰好加一，空批次不改变任何状态。
- **所有权**：所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可并发调用；返回的切片均为新分配的拷贝，调用方修改返回值不会影响队列内部状态。
- **复杂度**：Enqueue/Cancel 均摊 O(1)；`Apply` 为 O(k)（k 为批次大小，回滚同为 O(k)）；`Pop` 与 `Snapshot` 为 O(n log n)（n 为当前元素个数，主要来自排序），`Pop` 的就绪过滤为 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
