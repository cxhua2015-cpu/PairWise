# taskqueue135

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。架构分三层：

- `prioritybox.go` — 状态引擎：事务化数据与快照。
- `policy.go` — 准入策略：可原子替换的 actor 白名单 + 单批操作数上限。
- `coordinator.go` — 协调层：先授权再调用引擎，并为成功/拒绝/引擎失败分配连续审计序号。

## 状态引擎

- 显式非负单调时间：`Batch.Now < 0` 返回 `ErrInvalidInput`，小于当前时间返回 `ErrTime`。
- `Apply` 先做整批结构校验（kind、ID 字符集 `[a-z0-9_-]`、字节上限），再在单把互斥锁内对**候选事务**（克隆的 map 与 revision 计数器）顺序执行 Enqueue/Cancel；任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、状态、revision 全部回滚。容量只在末尾检查，因此同批 Cancel+Enqueue 可替换。
- 非空成功批次 generation 只 +1，空批次不变；revision 从 1 起单调分配。
- `Pop(now, limit)` 选取 `ReadyAt <= now` 的任务，按 Priority 降序、ReadyAt 升序、ID 升序排序，截取后在同一临界区内原子删除。

## 索引与复杂度

- 主索引为 `map[string]Item`（按 ID），Enqueue/Cancel 为 O(1) 均摊；候选事务克隆为 O(n)。
- 未维护堆：`Pop`/`Snapshot` 对候选集排序，复杂度 O(n log n)（n 为当前任务数）。队列规模受 `MaxItems` 约束，该权衡换取实现简单与严格的回滚语义。

## 所有权与并发

- 所有公开方法均可并发调用：引擎用 `sync.Mutex`，策略用 `sync.RWMutex`（`ReplaceActors` 整体换 map，读者无需拷贝），协调层审计日志用独立 `sync.Mutex`。
- `Snapshot`、`Pop`、`Decisions` 返回的切片均为新分配的副本，不与内部存储共享内存；调用方修改返回值不影响队列。
- 策略拒绝发生在读取核心状态之前，保证未授权请求不触碰引擎。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
