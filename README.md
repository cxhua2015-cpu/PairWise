# readyqueue365

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性检查、入队与取消。
- 弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选集即时排序，不维护持久堆；队列规模受 `MaxItems` 上限约束，排序开销可控。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与字节上限、非负时间），通过后才读取状态。
- 执行阶段以撤销日志（undo log）记录每个 Enqueue/Cancel 的逆操作；任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时逆序回放日志，并丢弃暂存的 `now`/`nextRevision` 增量，实现时间、状态与 revision 的完整回滚。
- 容量只在批次末尾检查，因此“先 Cancel 再 Enqueue”的替换批次在满队列上也能成功。
- 非空成功批次 `generation` 恰好加一；空批次不改变 generation。

### 所有权
- 所有共享状态由单把 `sync.Mutex` 保护，公开方法（`Apply`/`Pop`/`Snapshot`）可并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(k)，k 为批内操作数（不含末尾容量检查的 O(1) map 长度读取）。
- `Pop`：O(n log n)，n 为当前就绪（ReadyAt <= now）条目数。
- `Snapshot`：O(n log n)，n 为队列总条目数。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
