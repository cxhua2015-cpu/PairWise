# readyqueue310

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）由统一的 `less` 比较器定义；Pop 与 Snapshot 在持锁期间对候选切片就地排序，不维护额外的有序索引，避免与主索引产生一致性问题。

### 候选事务（Apply）
- 批次先做**完整结构校验**（时间非负且单调、kind 合法、ID 字符集/长度、ReadyAt 非负），期间不读取也不修改队列状态。
- 校验通过后按顺序应用 Enqueue/Cancel，同时记录 undo 日志（被覆盖/删除的旧值）与已分配的 revision 数。
- 任一步失败（`ErrExists`/`ErrNotFound`）或**末尾**最终容量检查失败（`ErrCapacity`）时，逆序回放 undo 日志并回退 revision 计数器，时间、状态、revision 全部回滚，队列保持原子性。
- 非空成功批次 generation 恰好加一；空批次不改变 generation，但仍推进单调时间。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；`Queue` 不可复制。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改返回值不会影响内部状态。
- Enqueue 的 revision 从 1 开始单调分配；`Result.Revision` 为最近一次分配的 revision，`Snapshot.NextRevision` 为下一个待分配值。

### 复杂度
- `Apply`：O(k)，k 为批内操作数（回滚同为 O(k)）。
- `Pop`：O(n log n)，n 为当前就绪候选数；删除为 O(k)。
- `Snapshot`：O(n log n)。
- `New`：O(1)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
