# taskqueue125

并发安全的内存型任务优先队列，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按任务 ID 提供 O(1) 的存在性判断、入队与取消。
- 不维护持久堆；`Pop`/`Snapshot` 时把候选条目物化为切片后按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，
  排序开销可控，实现更简单且不易出错。

### 候选事务（candidate transaction）
- `Apply` 先做整批结构校验（kind 合法、ID 字符集与字节上限、ReadyAt 非负、时间单调），
  再在一个克隆的 map 与克隆的 revision 计数器上顺序执行 Enqueue/Cancel。
- 容量检查只在所有操作执行完后进行一次（最终容量语义）。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选状态，
  时间、条目与 revision 计数器天然回滚；全部成功才一次性提交，
  非空批次 generation 恰好加一。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用。
- `Pop` 在选择与删除上持同一把锁，满足原子性。
- `Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个操作，当前 n 个条目）：结构校验 O(k·L)（L 为 ID 长度），
  克隆 O(n)，执行 O(k)，合计 O(n + k·L)。
- `Pop`（r 个就绪条目）：筛选 O(n)，排序 O(n log n)，删除 O(r)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
