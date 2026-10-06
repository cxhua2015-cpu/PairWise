# readyqueue280

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与时钟语义：

- `readyqueue280/prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`。
- `readyqueue280/validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共用 `validateBatchStructural`。
- `readyqueue280/stats.go` — 线性一致的状态摘要 `Stats`。
- `readyqueue280/clone.go` — 保留逻辑时钟、所有权完全隔离的深拷贝 `Clone`。

## 索引

- 任务以 `map[string]Item` 按 ID 索引，Enqueue/Cancel 查重与定位为 O(1)。
- 就绪选择不做额外堆索引：`Pop` 在持锁状态下扫描就绪候选（`ReadyAt <= now`），按
  Priority 降序、ReadyAt 升序、ID 升序排序后截取并原子删除。
- 容量上限（`MaxItems`）只在批次末尾检查；ID 字节上限（`MaxIDBytes`）属于结构校验。

## 候选事务

`Apply` 先在候选副本（scratch map + 本地 revision 计数）上顺序执行全部 Enqueue/Cancel，
最后统一检查容量；只有全部成功才把候选 map、revision、now、generation 一次性提交。
任何失败（`ErrExists` / `ErrNotFound` / `ErrCapacity` / `ErrTime`）直接丢弃候选，
时间、状态与 revision 天然回滚。非空成功批次 generation 恰好加一，空批次不改变任何状态。

## 所有权

- 所有公开方法共用同一把互斥锁，可并发调用；`Stats`/`Snapshot`/`Clone` 在锁内读取，满足线性一致。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，与内部状态隔离。
- `Clone` 复制全部逻辑时钟（generation、nextRevision、now）与全部 Item 到全新的 map 和锁，
  克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(k·n) 复制候选 + O(k) 执行（k 为批次大小，n 为当前元素数；候选复制为均摊 O(n)）。
- `Pop`：O(n log n)（扫描 + 排序就绪候选）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
