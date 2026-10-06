# readyqueue255

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构

实现刻意拆分为四个相互联动的文件，共享同一套语义：

- `readyqueue255/prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`，以及排序比较器与回滚日志。
- `readyqueue255/validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共用 `validateBatchStruct`，保证结构语义完全一致（kind 合法、Cancel 不得携带 Priority/ReadyAt、ID 字符集与字节上限、非负时间）。
- `readyqueue255/stats.go` — 线性一致统计：`Stats` 在同一把互斥锁下读取，返回与事务状态一致的快照摘要。
- `readyqueue255/clone.go` — 深拷贝：`Clone` 复制全部条目与逻辑时钟（generation、nextRevision、now），所有权完全独立。

## 索引与候选事务

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、Enqueue 查重与 Cancel 定位。
- `Apply` 是候选事务：先整体结构校验（不读状态），再在锁内按序执行 Enqueue/Cancel，期间记录 undo 日志；最终容量只在末尾检查。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）即逆序回放 undo 日志，回滚条目、revision 计数器与时间，队列保持进入批次前的状态。
- `Pop` 在候选集（`ReadyAt <= now`）上线性扫描，按 Priority 降序、ReadyAt 升序、ID 升序逐条选取并原子删除。

## 所有权与并发

- 所有公开方法经单一 `sync.Mutex` 串行化，可任意并发调用；`Stats`/`Snapshot`/`Clone` 因此是线性一致的。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，`Item` 为值类型，调用方无法借返回值触及内部状态；`Clone` 的 map 全新分配，两个队列互不影响。

## 复杂度

设 n 为队列中条目数、b 为批次大小、k 为单次 Pop 数量：

- `New` O(1)；`ValidateBatch` O(b·L)（L 为 ID 长度）。
- `Apply` O(b + n)（undo 日志与容量检查；map 操作均摊 O(1)）。
- `Pop` O(k·n) 选择 + O(k) 删除；`Snapshot` O(n log n)（排序）；`Stats` O(1)；`Clone` O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
