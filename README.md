# readyqueue350

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 不维护持久堆：`Pop`/`Snapshot` 时按需收集候选并排序（Priority 降序、ReadyAt 升序、ID 升序）。在 `MaxItems` 受限的场景下，这避免了堆修复与懒删除的复杂度，且排序结果天然是规范顺序。

### 候选事务
- `Apply` 先对整个批次做纯结构校验（时间非负且单调、kind 合法、ID 字符集与字节上限、ReadyAt 非负），此阶段不触碰任何状态。
- 随后在持锁状态下顺序执行 Enqueue/Cancel，并记录撤销日志（被覆盖/删除的旧值、已分配的 revision 计数）。任一步失败（`ErrExists`/`ErrNotFound`）或最终容量超限（`ErrCapacity`）时，按逆序回放撤销日志并回退 `nextRev`，时间、状态与 revision 全部复原。
- 容量检查只在批次末尾进行，因此“先 Cancel 再 Enqueue”的替换批次不会触发容量错误。
- 非空成功批次 `generation` 恰好加一；空批次是完全无操作，不改变 generation 与时间。

### 所有权
- 所有公开方法通过单一 `sync.Mutex` 串行化，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响队列内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
设 n 为当前元素数、k 为批次操作数、m 为就绪候选数：
- `New`：O(1)。
- `Apply`：O(k) 校验与执行，回滚同为 O(k)；末尾容量检查 O(1)。
- `Pop`：O(n) 扫描就绪项 + O(m log m) 排序 + O(min(m, limit)) 删除。
- `Snapshot`：O(n log n) 排序并拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
