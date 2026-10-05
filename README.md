# taskqueue195

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 不维护持久堆；Pop/Snapshot 时按需对候选集做规范排序（Priority 降序、ReadyAt 升序、ID 升序）。队列规模受 `MaxItems` 上限约束，惰性排序比维护堆的簿记更简单且无正确性风险。

**候选事务（Apply）**
- 批次先做完整结构校验（`Now >= 0`、kind 合法、ID 字符集/长度、ReadyAt 非负），不读取任何状态。
- 加锁后检查单调时间（`Now >= q.now`，否则 `ErrTime`），随后在主索引的克隆（候选 map）上顺序执行 Enqueue/Cancel，revision 从候选计数器分配。
- 容量只在末尾检查一次：`|candidate| > MaxItems` 则整个批次失败。
- 任一步失败直接丢弃候选，时间、状态、revision、generation 全部自然回滚；成功才一次性提交，非空批次 generation 恰好 +1，空批次为无操作。

**所有权与并发**
- 所有公开方法经同一把 `sync.Mutex` 串行化，可任意并发调用。
- 返回的 `[]Item` 与 `Snapshot` 均为新分配的副本，调用方修改不影响内部状态；队列不保留调用方传入的切片。
- 时间为显式非负单调时钟：成功的 Apply/Pop 将队列时间推进到批次的 `Now`/Pop 的 `now`，失败不推进。

**复杂度**（n = 当前任务数，b = 批次大小，k = 就绪任务数）
- `Apply`：结构校验 O(b)，候选克隆 O(n)，执行 O(b)，总计 O(n + b)。
- `Pop`：扫描 O(n)，排序 O(k log k)，删除 O(min(k, limit))。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
