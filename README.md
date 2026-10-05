# taskqueue085

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按任务 ID 提供 O(1) 的存在性判断、入队与取消。
- 不维护有序堆：`Pop`/`Snapshot` 时对候选集按（Priority 降序、ReadyAt 升序、ID 升序）做一次性排序。队列规模受 `MaxItems` 上限约束，排序成本可控，且实现简单、无堆修复路径出错的风险。

### 候选事务
- `Apply` 先做完整结构校验（kind、ID 字符集与长度、非负时间），不读取任何状态。
- 随后在互斥锁内将主索引克隆为候选副本，按顺序在副本上执行 Enqueue/Cancel，revision 从候选计数器分配；容量只在所有操作执行完毕后做最终检查。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）直接丢弃候选副本，时间、状态、revision、generation 全部天然回滚；成功才一次性提交并令 generation 恰好 +1。空批次不改变任何状态。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改返回结果不会影响队列内部状态，内部状态也不会因后续调用而修改已返回的数据。

### 复杂度
- `Apply`：O(k·n) 克隆 + O(k) 操作（k 为批大小，n 为当前任务数）；结构校验 O(k·L)（L 为 ID 长度）。
- `Pop`：O(n + r log r)，r 为就绪任务数；删除 O(r)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
