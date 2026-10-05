# taskqueue155

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的 `ErrExists` 判重与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时把候选条目收集到切片并按规范序（Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，排序开销可控，且实现简单、无堆与 map 双写不一致风险。

### 候选事务
- `Apply` 分三段：先做完整结构校验（`Now >= 0`、kind 合法、ID 字符集与字节上限、`ReadyAt >= 0`），不读取任何状态；再检查时间单调性（`ErrTime`）；随后在**候选副本**（克隆的 map 与本地 revision 计数）上顺序执行 Enqueue/Cancel。
- 容量只在末尾检查一次：候选集大小超过 `MaxItems` 则整批返回 `ErrCapacity`。
- 任一失败直接丢弃候选，时间、状态、revision 天然回滚；全部成功才一次性提交，非空批次 generation 恰好加一，空批次不变。

### 所有权与并发
- 单个 `sync.Mutex` 保护全部内部状态，所有公开方法可并发调用；临界区内无阻塞调用。
- 返回值所有权独立：Snapshot 的 `Items` 与 Pop 的结果都是新分配的切片，元素为值拷贝，调用方修改不影响内部状态。

### 复杂度
- `Apply`：O(n + k)，n 为当前条目数（克隆候选），k 为批次内 op 数。
- `Pop`：O(n log n)（筛选 ReadyAt <= now 后排序），删除为 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
