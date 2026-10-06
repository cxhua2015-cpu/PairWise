# readyqueue215

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护有序堆；Pop/Snapshot 时按需对候选集排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。

**候选事务**
- `Apply` 先在持有锁的情况下对整个批次做纯结构校验（kind、ID 字符集与字节上限、ReadyAt 非负、时间非负且不回退），不触碰任何状态。
- 校验通过后在现有 map 的副本上顺序执行 Enqueue/Cancel（候选事务）；任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时直接丢弃副本，时间、状态与 revision 计数器全部天然回滚。
- 仅当全部操作成功且最终容量不超限才一次性提交：替换 map、推进时间、提交 revision 计数器，非空批次 generation 恰好加一；空批次不改变任何状态。

**所有权与并发**
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用。
- `Snapshot` 与 `Pop` 返回的切片均为新分配的 `Item` 值拷贝，调用方修改不影响内部状态。

**复杂度**（n = 队列中元素数，b = 批次大小，k = Pop 的 limit）
- `Apply`：O(n + b)，复制 map 加逐操作 O(1)。
- `Pop`：O(n log n)，筛选 `ReadyAt <= now` 后排序，原子删除至多 k 个。
- `Snapshot`：O(n log n)，拷贝并排序。
- 空间：O(n)。
