# taskqueue100

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主存储为 `map[string]Item`，以任务 ID 为键，Enqueue/Cancel/去重均为 O(1) 均摊查找。
- 不维护持久堆；Pop/Snapshot 时按需收集并排序。该规模下实现更简单，且 Pop 只需对 `ReadyAt <= now` 的候选子集排序。

### 候选事务（Apply）
- Apply 在单个互斥锁内执行：先做整批结构校验（kind、ID 字符集与字节上限、ReadyAt 非负、Now 非负），再检查时间单调性，然后顺序执行 Enqueue/Cancel。
- 执行期间记录撤销日志（新增 ID 列表、被删除的 Item 副本）并保存进入时的 `nextRevision`；任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）即回滚：恢复 map、revision 计数器，时间戳与 generation 保持不变。
- 容量只在批次末尾检查，因此批内可先超后降（先 Cancel 再 Enqueue 可通过）。
- 非空成功批次 generation 恰好 +1；空批次不改变 generation，但成功批次会推进队列时间。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可安全并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不影响内部状态；`Item` 为值类型，无共享指针。
- 队列时间为显式非负单调值：Apply/Pop 的 `now` 小于当前时间返回 `ErrTime`，负数返回 `ErrInvalidInput`，失败调用不推进时间。

### 复杂度
设 n 为队列中任务数，b 为批次内操作数，r 为就绪候选数，k 为 Pop 上限：
- `New`：O(1)
- `Apply`：O(b)（校验 + map 操作 + 可能的回滚）
- `Pop`：O(n + r log r)，另加 O(k) 删除
- `Snapshot`：O(n log n)（复制并按规范顺序排序）

排序顺序统一为：Priority 降序、ReadyAt 升序、ID 升序。
