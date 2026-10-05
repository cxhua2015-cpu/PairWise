# settlementqueue

Read `SPEC.md` and implement the package.

并发安全的内存型结算优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、Enqueue 去重与 Cancel 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久有序结构，而是在 `Pop`/`Snapshot` 时对候选切片就地排序；队列规模受 `MaxItems` 上限约束，排序代价可控。

### 候选事务
- `Apply` 先对整个批次做纯结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态。
- 随后在锁内把当前 map 复制为候选副本，按顺序在副本上执行 Enqueue/Cancel，revision 计数器也使用局部副本。
- 容量只在所有操作执行完后检查一次；任一步失败直接丢弃候选副本，时间、状态、revision、generation 全部保持原值（天然回滚）。
- 全部成功才一次性提交：替换 map、推进 `now`、提交 revision 计数器，非空批次 generation 恰好加一；空批次不改变任何状态。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Item` 为纯值类型，无共享指针。
- `Pop` 在锁内完成“选择 + 删除”，对单个队列实例而言是原子的。

### 复杂度
设 n 为当前队列长度，k 为批次操作数，m 为就绪任务数：
- `New`：O(1)。
- `Apply`：结构校验 O(k·L)（L 为 ID 长度）；候选复制 O(n)；执行 O(k)；末尾容量检查 O(1)。总计 O(n + k·L)。
- `Pop`：筛选 O(n)，排序 O(m log m)，删除 O(limit)。
- `Snapshot`：O(n log n)（复制 + 规范序排序）。
