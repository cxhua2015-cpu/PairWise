# taskqueue195

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、插入与删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久堆，而是在 `Pop`/`Snapshot` 时对候选集即时排序，保证实现简单且顺序始终一致。

### 候选事务
- `Apply` 先做完整结构校验（时间非负、kind 合法、ID 字符集与长度），不读取任何状态。
- 通过校验后在锁内克隆当前 `items` 与 `nextRevision` 得到候选状态，顺序执行 Enqueue/Cancel；容量只在末尾对候选大小检查一次。
- 任一步失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、状态、revision、generation 全部不变；成功才整体提交。非空成功批次 generation 恰好 +1，空批次不变。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 串行化，可任意并发调用。
- `Pop` 在选择后原子删除所选项；返回的切片为新分配内存。
- `Snapshot` 返回的 `Items` 是独立副本，调用方修改不会影响队列内部状态。

### 复杂度
- `Apply`：O(k·n) 克隆 + O(k) 执行（k 为批大小，n 为当前元素数；克隆为 map 浅拷贝）。
- `Pop`：O(n log n) 排序就绪集，删除 O(limit)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
