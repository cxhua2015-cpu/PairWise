# readyqueue380

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引与所有权
- 内部权威存储为 `map[string]Item`（按 ID 唯一索引），由单个 `sync.Mutex` 保护；所有公开方法（`Apply`/`Pop`/`Snapshot`）均在该锁下执行，可任意并发调用。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不持久化维护，而是在 `Pop`/`Snapshot` 时对候选集合即时排序，避免堆/树结构在部分回滚下的复杂性。
- 所有权：返回给调用方的切片（`Pop` 结果、`Snapshot().Items`）均为新分配的副本，与内部状态完全隔离；调用方修改不影响队列。`Item` 为纯值类型，无共享指针。

### 候选事务（Apply）
1. 先对整个批次做纯结构校验（`Now >= 0`、kind 合法、ID 非空且仅含 `[a-z0-9-_]` 且不超过 `MaxIDBytes`），不读取任何状态。
2. 加锁后检查时间单调性（`Now < q.now` → `ErrTime`）。
3. 克隆当前 map 得到候选状态，在候选上顺序执行 Enqueue（分配自增 revision，重复 ID → `ErrExists`）/ Cancel（缺失 ID → `ErrNotFound`）。
4. 最终容量只在末尾检查一次（超过 `MaxItems` → `ErrCapacity`），因此同批次内“先 Cancel 再 Enqueue”可以合法通过。
5. 任一步失败直接丢弃候选：时间、条目、revision 计数器全部保持原值，实现天然回滚。成功才一次性提交，且非空批次 generation 只增加一次；空批次不改变任何状态。

### Pop
校验 `now >= 0`、`n > 0`，检查时间单调后，筛出 `ReadyAt <= now` 的条目，按规范顺序排序取前 `n` 个并原子删除。失败（参数非法或时间倒退）不修改任何状态。

### 复杂度
- `Apply`：O(m·k)，m 为批内操作数、k 为当前条目数（候选克隆 O(k)，每步 map 操作 O(1)）。
- `Pop`：O(k + r log r)，r 为就绪条目数。
- `Snapshot`：O(k log k)。
- 空间：O(k)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
