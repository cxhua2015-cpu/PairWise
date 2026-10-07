# readyqueue365

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 `Enqueue` 去重（`ErrExists`）与 `Cancel` 查找（`ErrNotFound`）。
- `Pop`/`Snapshot` 时按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选切片排序；`Pop` 先过滤 `ReadyAt <= now` 的就绪项。队列规模受 `MaxItems` 约束，排序开销可控，避免了堆索引与“就绪过滤”组合的复杂性。

### 候选事务
- `Apply` 先在持有锁的情况下对整个批次做**完整结构校验**（kind、ID 字符集与字节上限、`Now >= 0`），再检查时间单调性（`ErrTime`）。
- 随后在**条目副本**（候选事务）上顺序执行 Enqueue/Cancel：Enqueue 从单调计数器分配 revision，Cancel 删除条目；**最终容量只在末尾检查**（`ErrCapacity`）。
- 任一步失败即丢弃副本，时间、条目与 revision 计数器全部回滚；成功才一次性提交，且非空批次 generation 只增加一次，空批次不改变任何状态。

### 所有权与并发
- 所有公开方法经单一 `sync.Mutex` 串行化，可安全并发调用。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，调用方修改不影响内部状态；`Item` 为值类型，无共享指针。

### 复杂度
设 n 为队列中条目数、k 为批次操作数：
- `New`：O(1)。
- `Apply`：校验 O(k·L)（L 为 ID 长度），候选事务复制 O(n)，执行 O(k)，合计 O(n + k·L)。
- `Pop`：过滤 O(n) + 排序 O(n log n)，删除 O(n)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
