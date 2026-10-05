# taskqueue180

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists` / `ErrNotFound`）。
- 就绪候选不维护持久堆，而是在 `Pop`/`Snapshot` 时对当前条目做一次性筛选与排序
  （Priority 降序、ReadyAt 升序、ID 升序）。条目数为受控的 `MaxItems`，
  该设计以简单性换取足够的确定性顺序，避免堆与 map 双索引的一致性负担。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与长度、非负时间），不读取任何状态。
- 随后在单把互斥锁内把整批操作暂存到候选事务（staged map，含取消墓碑），
  顺序执行 Enqueue/Cancel 并预分配 revision；最终容量只在末尾检查一次。
- 任一步失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃暂存区，
  时间、条目与 revision 计数器全部保持原值，实现原子回滚；只有全部成功才提交，
  非空成功批次 generation 恰好加一，空批次不改变任何状态。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用。
- `Pop` 在选择的同时原子删除，返回的 `Item` 为值拷贝；`Snapshot` 每次新建切片，
  调用方对返回切片的任何修改都不会影响队列内部状态。

### 复杂度
设 n 为当前条目数，k 为批次内操作数：
- `New`：O(1)。
- `Apply`：结构校验 O(k·L)（L 为 ID 长度），执行与提交 O(k)。
- `Pop`：筛选 O(n)，排序 O(n log n)，删除 O(min(limit, n))。
- `Snapshot`：O(n log n)，返回独立副本。
