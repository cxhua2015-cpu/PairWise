# readyqueue355

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引设计

- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时按需对候选集做一次性排序（Priority 降序、ReadyAt 升序、ID 升序）。队列规模受 `MaxItems` 上限约束，惰性排序比维护堆的常数更简单且无额外不变量。

## 候选事务（Apply）

- Apply 先做整批结构校验（时间非负、kind 合法、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态。
- 之后在互斥锁内检查单调时间，再顺序执行 Enqueue/Cancel；Enqueue 从 `nextRevision` 分配递增 revision。
- 每个操作记录一条 undo 日志；任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，逆序回放 undo 并回退 `nextRevision`，时间、状态、revision 全部回到批次之前。
- 容量只在批次末尾检查，因此同批内“先 Cancel 再 Enqueue”可以越过瞬时超限。
- 非空成功批次 generation 恰好 +1；空批次不改变 generation，但仍推进时间。

## 所有权与并发

- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；锁内无阻塞 I/O。
- `Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不影响内部状态。
- Pop 在选择与删除上原子完成：同一批就绪项要么全部被本次 Pop 取走，要么不被触碰。

## 复杂度

设 n 为队列内元素数，b 为批次内操作数，k 为 Pop 的就绪候选数：

- `New`：O(1)
- `Apply`：结构校验 O(b·L)（L 为 ID 长度），执行 O(b)，末尾容量检查 O(1)；回滚 O(b)
- `Pop`：筛选 O(n)，排序 O(k log k)，删除 O(min(k, limit))
- `Snapshot`：O(n log n)

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
