# resourcelease124

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：内部使用单个 `map[string]Entry` 按 key 索引，Put/Touch/Delete 均为 O(1) 均摊。淘汰（Apply 前置驱逐与 `Expire`）需要全表扫描，为 O(n)；`Snapshot` 复制并排序，为 O(n log n)。未维护额外的堆/时间轮索引，因为在每次 Apply 前都需要按闭区间 `ExpiresAt <= Now` 驱逐，map 扫描已足够且实现简单。

**候选事务**：`Apply` 先在互斥锁外做整批结构校验（kind、key 字符集与长度、非负时间戳），再在锁内检查时间单调性。通过后把当前 map 复制为候选状态，先在候选上驱逐过期条目，再顺序执行 Put/Touch/Delete（Put/Touch 各分配一个递增 revision）。任何错误（`ErrNotFound`、`ErrCapacity` 等）发生时直接丢弃候选，已发生的驱逐、时间与 revision 分配全部随候选一起回滚，表保持 Apply 前的状态。仅在全部成功后才一次性提交候选 map、时间、revision 计数器；非空批次 generation 恰好加一，空批次不变。

**所有权**：`Table` 全部状态（map、now、generation、nextRevision）由一把 `sync.Mutex` 保护，所有公开方法可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，与内部状态完全隔离，调用方可自由修改。

**复杂度**：Put/Touch/Delete 单操作 O(1) 均摊；`Apply` 为 O(n + m)（n 为表大小、m 为批大小，含候选复制与驱逐扫描）；`Expire` O(n)；`Snapshot` O(n log n)；空间 O(n)。
