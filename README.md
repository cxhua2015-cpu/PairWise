# readyqueue420

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。队列使用显式非负单调时间：`Apply`/`Pop` 携带的 `now` 不得小于当前逻辑时钟，否则返回 `ErrTime`。

## 公开语义

- `Apply` 原子顺序执行批次中的 `Enqueue`/`Cancel`：`Enqueue` 分配单调递增的 revision，容量上限只在批次末尾检查；任一操作失败则整体回滚时间、元素集合、revision 与 generation。非空成功批次 generation 恰好加一，空批次不变。
- `Pop(now, n)` 选取 `ReadyAt <= now` 的任务，按 Priority 降序、ReadyAt 升序、ID 升序返回并原子删除。
- 批次先做完整结构校验（kind、ID 字符集 `[a-z0-9_-]`、字节上限、Cancel 不得携带 Priority/ReadyAt、非负时间），再触碰状态；`Apply` 与 `ValidateBatch` 共享同一套结构语义。

## 架构（多文件）

- `prioritybox.go` — 核心事务引擎：`Queue` 持有一把 `sync.Mutex` 保护全部可变状态（`items`、`now`、`generation`、`nextRevision`），所有公开方法并发安全。
- `validation.go` — 无副作用批次预检：只读不可变的 `Options`，不读不改队列状态。
- `stats.go` — 线性一致统计：在同一临界区内读取时钟与计数，返回一致快照。
- `clone.go` — 深拷贝：复制逻辑时钟与全部元素，克隆体持有独立锁与 map，所有权完全隔离。

## 索引

元素存储为 `map[string]Item`，按 ID 提供 O(1) 存在性判断（`ErrExists`/`ErrNotFound`）与删除。规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序生成，避免为每次入队维护堆结构。

## 候选事务

`Apply` 在候选 map（现有元素的浅拷贝）上顺序执行全部操作，末尾做容量检查；任何失败直接丢弃候选并返回错误，原状态、时间与 revision 计数器从未被触碰，因此回滚是免费的。全部成功才一次性提交候选 map、时钟与计数器。

## 所有权

`Snapshot`/`Pop` 返回的切片均为新建副本，与内部 map 完全隔离；`Clone` 复制全部元素并分配新锁，克隆体与原队列互不影响。`Item` 为纯值类型，无共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为现有元素数（候选拷贝），m 为批次操作数。
- `Pop`：O(n log n)，n 为就绪元素数（过滤 + 排序）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
