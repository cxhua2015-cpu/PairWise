# readyqueue370

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引结构

队列内部为每个任务保存一份 `entry`（含 `Item` 与堆下标），并维护两个索引：

- **ID 索引** `map[string]*entry`：O(1) 判重（`ErrExists`）与按 ID 取消（`ErrNotFound`）。
- **就绪堆** `container/heap`，按 `ReadyAt` 升序（平级按 Priority 降序、ID 升序）：堆顶即最早就绪候选，Pop 时只需取出 `ReadyAt <= now` 的条目。

## 候选事务

- **Apply**：先对整个批次做完整结构校验（kind、ID 字符集与字节上限、`ReadyAt >= 0`、`Now >= 0`），再检查时间单调性（`ErrTime`）。随后顺序执行 Enqueue/Cancel，Enqueue 就地分配单调递增的 revision，并记录撤销日志（undo log）。任一操作失败或**末尾**容量检查（`len > MaxItems`）失败时，按逆序回放撤销日志并恢复 revision 计数器，时间、状态与 revision 全部回滚，批次原子失败。非空成功批次 generation 恰好加一；空批次不改变 generation。
- **Pop**：在锁内从就绪堆取出全部 `ReadyAt <= now` 的候选，按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序后取前 `limit` 个，从 ID 索引中删除并返回；未选中的候选重新压回堆。选择、删除与提交时间在同一临界区内原子完成。

## 所有权

所有公开方法返回的切片（`Pop` 结果、`Snapshot().Items`）都是新分配的拷贝，调用方可自由修改，不会别名队列内部状态；队列独占其内部 `entry` 数据。全部公开方法由同一把 `sync.Mutex` 保护，可安全并发调用。

## 复杂度

设 n 为队列内任务数，b 为批次操作数，k 为 Pop 的 limit，r 为就绪候选数：

- `New`：O(1)。
- `Apply`：O(b log n)，每次 Enqueue/Cancel 为一次堆插入/删除；回滚代价与同批操作同阶。
- `Pop`：O(r log n + r log r)，取出 r 个就绪候选、排序并回堆未选中项；删除本身 O(k)。
- `Snapshot`：O(n log n)，拷贝并按规范顺序排序。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
