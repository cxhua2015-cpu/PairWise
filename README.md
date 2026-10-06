# readyqueue230

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构与索引

- `readyqueue230/prioritybox.go` — 核心事务引擎：`Queue` 以 `map[string]Item` 为主索引（按 ID 精确查找/去重，O(1)），配一把 `sync.Mutex` 保护全部状态（`now`、`generation`、`nextRevision`、`items`）。排序键（Priority 降序、ReadyAt 升序、ID 升序）在读取路径上按需计算，不维护额外堆结构。
- `readyqueue230/validation.go` — 无副作用的批次结构预检 `ValidateBatch`：校验 `Now >= 0`、kind 合法、ID 字符集（`[a-z0-9-_]`、非空、不超过 `MaxIDBytes` 字节）、Enqueue 的 `ReadyAt >= 0`、Cancel 的 Priority/ReadyAt 必须为 0。`Apply` 复用同一套结构语义，预检通过后才读取状态。
- `readyqueue230/stats.go` — `Stats` 在同一临界区内读取逻辑时钟与条目数，提供线性一致的状态摘要。
- `readyqueue230/clone.go` — `Clone` 在锁内复制 map 与逻辑时钟（now/generation/nextRevision），返回完全独立所有权的深拷贝；克隆体与原队列互不影响。

## 候选事务

`Apply` 先在条目 map 的私有副本上顺序执行 Enqueue/Cancel：Enqueue 分配递增 revision（重复 ID 报 `ErrExists`），Cancel 缺失报 `ErrNotFound`；最终容量只在末尾检查（`ErrCapacity`）。任一失败即丢弃副本——时间、状态与 revision 计数全部回滚；成功才一次性提交并将 `now` 推进到 `Batch.Now`、generation 恰好加一（空批次不变）。`Pop(now, limit)` 在同一临界区内筛选 `ReadyAt <= now` 的候选、排序并原子删除，同时推进时钟；`now < 0` 或 `limit < 1` 报 `ErrInvalidInput`，时钟回退报 `ErrTime`。

## 所有权

所有公开方法并发安全。`Snapshot`/`Pop` 返回的切片均为新建副本，`Clone` 深拷贝全部条目，调用方与内部状态完全隔离。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（复制 map），m 为批内操作数。
- `Pop`：O(n log n)（筛选 + 排序就绪候选）。
- `Snapshot`：O(n log n)；`Stats`/`ValidateBatch`：O(1)/O(m)。
- `Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
