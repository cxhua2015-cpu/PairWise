# taskqueue190

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主存储为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断（Enqueue 查重、Cancel 定位）。
- 不维护持久堆；`Pop`/`Snapshot` 时对当前条目按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）现排。容量有界（`MaxItems`），排序开销可控，实现简单且无堆修复路径。

### 候选事务（candidate transaction）
- `Apply` 先完整结构校验（kind、ID 字符集与字节上限、非负 ReadyAt、非负且单调的 Now），再读取状态。
- 通过后在队列当前 map 的克隆上顺序执行 Enqueue/Cancel，revision 从局部 `nextRevision` 起分配；容量只在所有操作执行完后检查一次。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃克隆：时间、状态、revision 计数器全部天然回滚。成功才一次性提交 map、时间、revision，且非空批次 generation 只加一，空批次不变。

### 所有权与并发
- 所有公开方法经单把 `sync.Mutex` 串行化，可任意并发调用。
- `Snapshot`/`Pop` 返回的切片均为新建副本，调用方修改不影响内部状态；队列也不保留调用方传入的切片。
- 显式时间非负且单调：`Now`/`now` 小于当前时间返回 `ErrTime`，负数返回 `ErrInvalidInput`，失败不推进时间。

### 复杂度（n = 当前条目数，b = 批次操作数，k = Pop limit）
- `Apply`：校验 O(b·L)（L 为 ID 长度），执行 O(n + b)（克隆 + 哈希操作），空间 O(n)。
- `Pop`：排序 O(n log n)，筛选并删除 O(n)，返回至多 k 项。
- `Snapshot`：O(n log n)，返回副本。
- `New`：O(1)。
