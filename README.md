# readyqueue415

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义以 `SPEC.md` 与契约测试为准。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义：

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共用 `validateBatchStructure`，先完整结构校验再读取状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下一次性采样逻辑时钟与条目数。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 保留逻辑时钟（now / generation / nextRevision）。

## 索引

- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 弹出时在候选集上按规范序（Priority 降序、ReadyAt 升序、ID 升序）排序；`Snapshot` 亦按该序返回。

## 候选事务

- `Apply` 先在副本 map 上顺序执行 Enqueue/Cancel（Enqueue 分配单调递增 revision），最终容量只在末尾检查；任一失败（`ErrTime` / `ErrExists` / `ErrNotFound` / `ErrCapacity`）直接丢弃候选副本，时间、状态与 revision 计数全部回滚。
- 非空成功批次 generation 只增加一次，空批次不变；批次时间在提交时单调前进。
- `Pop(now, limit)` 原子地选出 `ReadyAt <= now` 的任务、按规范序截取并删除，同时推进时钟。

## 所有权

- 所有公开方法由单个 `sync.RWMutex` 保护，可并发调用；写操作独占，读操作（`Snapshot` / `Stats` / `Clone`）共享读锁。
- 返回的切片均为新建副本，`Item` 为值类型；`Clone` 重建底层 map，与原队列完全隔离，互不影响。

## 复杂度

- `Apply`：O(n + b)，n 为当前条目数（复制候选 map），b 为批次内操作数。
- `Pop`：O(n + r log r)，r 为就绪候选数。
- `Snapshot` / `Clone`：O(n log n) / O(n)；`Stats`：O(1)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
