# readyqueue305

并发安全的内存型“就绪优先队列”，语义见 `SPEC.md`。Go 1.22+，仅标准库。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、Enqueue 去重与 Cancel 删除。
- 弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，而是在 `Pop`/`Snapshot` 时对候选集一次性排序；容量受 `MaxItems` 上限约束，排序开销有界。

**候选事务**
- `Apply` 先在锁外做整批结构校验（kind、ID 字符集与字节上限、非负时间），再在锁内把时间快照外的条目复制到候选 map 上顺序执行 Enqueue/Cancel。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时直接丢弃候选，时间、条目与 revision 计数器全部保持原值，实现原子回滚。
- 仅当整批成功时才一次性提交：替换条目 map、推进单调时间、generation 恰好加一（空批次只推进时间，不变 generation）、提交 revision 计数器。

**所有权**
- 所有公开方法通过同一把 `sync.Mutex` 串行化，可任意并发调用。
- 存入队列的 `Item` 为值拷贝；`Pop`/`Snapshot` 返回的切片均为新建数组，调用方修改返回结果不会影响内部状态。

**复杂度**（n 为当前条目数，b 为批大小，k 为弹出上限）
- `Apply`：校验 O(b)，候选复制 O(n + b)，末尾容量检查 O(1)。
- `Pop`：筛选 O(n)，排序 O(n log n)，删除 O(k)。
- `Snapshot`：O(n log n)。空间 O(n)。
