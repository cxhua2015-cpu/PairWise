# readyqueue370

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。实现见 `readyqueue370/prioritybox.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

- **索引**：主索引为 `map[string]Item`（ID → 条目），Enqueue/Cancel/存在性判断均为 O(1)。Pop 与 Snapshot 时把条目拷入切片并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，避免维护堆带来的删除复杂度。
- **候选事务**：`Apply` 先做完整结构校验（时间非负单调、kind 合法、ID 字符集与字节上限），再把当前 map 克隆为候选副本，在其上顺序执行 Enqueue（分配递增 revision）/Cancel，最后才检查容量。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，时间、状态、revision 计数器全部保持不变，实现原子回滚；成功则一次性提交并令 generation 恰好 +1（空批次不变）。
- **所有权**：所有公开方法共用一把 `sync.Mutex`，可任意并发调用。`Pop` 在锁内选择 `ReadyAt <= now` 的条目并原子删除；`Snapshot`/`Pop` 返回的切片均为新建副本，调用方修改不影响内部状态。`Pop` 同样推进单调时钟（`now < 当前` 返回 `ErrTime`，`now < 0` 或 `limit <= 0` 返回 `ErrInvalidInput`）。
- **复杂度**：Enqueue/Cancel 单op均摊 O(1)，但 `Apply` 因候选克隆为 O(n + k)（n 为现存条目数，k 为批内 op 数）；`Pop`/`Snapshot` 为 O(n log n)（排序）；空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
