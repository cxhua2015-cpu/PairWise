# taskqueue105

并发安全的内存型任务优先队列,语义见 `SPEC.md`。Go 1.22+,仅标准库。

## 设计说明

### 索引
- `map[string]entry`:ID → 任务的主索引,负责存在性判断(`ErrExists`/`ErrNotFound`)与容量计数,O(1)。
- `readyHeap`:按 `ReadyAt` 升序(平局回退到规范顺序)的二叉堆,仅服务于 Pop 的就绪候选筛选。
  注意不能用优先级作堆键:高优先级但未就绪的任务会挡住低优先级已就绪任务。

### 候选事务
- `Apply` 先在持锁状态下做完整结构校验(不读状态),再顺序执行 Enqueue/Cancel;
  每个操作记录 undo 日志,失败(含末尾容量检查 `ErrCapacity`)时逆序回滚状态与
  `nextRevision`,时间不前进,generation 不变。非空成功批次 generation 恰好 +1。
- `Pop` 从事故堆中取出全部 `ReadyAt <= now` 的候选,按规范顺序(优先级降序、
  ReadyAt 升序、ID 升序)排序后取前 n 个原子删除,其余候选压回堆中。

### 所有权
- 所有公开方法持有同一把 `sync.Mutex`,可并发调用。
- `Snapshot`/`Pop` 返回的切片均为新分配的副本,调用方修改不影响内部状态。

### 复杂度(N = 队列大小,K = 批次操作数,R = 就绪任务数)
- `New`:O(1)。
- `Apply`:O(K + N log N)(末尾重建堆;回滚同为 O(K + N log N))。
- `Pop`:O(R log N + R log R)。
- `Snapshot`:O(N log N)(规范序排序的独立副本)。
