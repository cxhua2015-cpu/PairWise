# deliveryqueue

并发安全的内存型投递优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）。
- 不维护持久堆：Pop/Snapshot 时现取候选并排序，避免堆与 map 双索引的一致性开销；队列规模受 `MaxItems` 上限约束，排序代价可控。
- 规范顺序：Priority 降序 → ReadyAt 升序 → ID 升序，Pop 与 Snapshot 共用同一比较器。

### 候选事务（Apply）
- 批次先做完整结构校验（Now/ReadyAt 非负、Kind 合法、ID 字符集与字节上限），通过后才读取状态，保证错误优先级确定。
- 在单把互斥锁内顺序执行 Enqueue/Cancel：Enqueue 分配递增 revision，Cancel 删除条目；期间记录 undo 日志（新增 ID / 被删条目及起始 revision）。
- 容量只在所有操作执行完毕后检查一次（`ErrCapacity`），因此“先 Cancel 再 Enqueue”可在满队列中成功。
- 任一失败（时间回退、重复、缺失、超容量）即回滚：撤销全部条目变更并恢复 `nextRevision`，时间与 generation 在提交前不写入，天然不变。
- 提交时推进单调时间；非空成功批次 generation 恰好 +1，空批次不变。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用；`New` 返回的队列不与其他队列共享状态。
- `Item` 为纯值类型；`Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不会影响内部状态，内部后续变更也不影响已返回的切片。

### 复杂度
- `Apply`：结构校验 O(Σ|ID|)，执行 O(k)，容量检查 O(1)；失败回滚 O(k)（k 为批内操作数）。
- `Pop`：O(n log n)，n 为当前条目数（筛选 ReadyAt ≤ now 后排序，取前 limit 个并原子删除）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
