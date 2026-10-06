# readyqueue225

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。队列使用显式非负单调时间：`Apply` 原子顺序执行 Enqueue/Cancel，`Pop` 弹出 `ReadyAt <= now` 的任务，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。

## 多文件架构

- `prioritybox.go` — 核心事务引擎：类型、错误值、`New`/`Apply`/`Pop`/`Snapshot`。
- `validation.go` — 无副作用的批次结构预检；`Apply` 与 `ValidateBatch` 共享同一套 `validateBatch` 语义（kind 合法、ID 字符集与字节上限、Cancel 不得携带 Priority/ReadyAt、时间非负）。
- `stats.go` — 线性一致的 `Stats`：与事务同一把互斥锁下读取，保证快照语义。
- `clone.go` — 深拷贝 `Clone`：复制逻辑时钟（now/generation/nextRevision）与全部条目，所有权完全隔离。

## 索引与候选事务

- 条目存储于 `map[string]Item`（按 ID 索引，O(1) 查找）；弹出前对候选切片按规范顺序排序。
- `Apply` 采用候选事务：先在条目副本上顺序执行全部操作，任一失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时直接丢弃候选，时间、条目集与 revision 计数器全部回滚；成功才一次性提交并递增 generation（空批次不递增）。

## 所有权

- 所有公开方法持有同一互斥锁，可并发调用。
- `Snapshot`/`Pop` 返回的切片均为新建副本，与内部状态隔离；`Clone` 复制 map，克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(n + k)，n 为当前条目数（候选拷贝），k 为批次数。
- `Pop`：O(n + r log r)，r 为就绪候选数。
- `Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(1)。
