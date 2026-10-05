# settlementqueue

并发安全的内存型结算优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、Enqueue 去重与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时把条目拷贝到临时切片并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。条目数为 n 时排序成本 O(n log n)，换取实现简单与无索引不一致风险。

### 候选事务（Apply）
- Apply 先做整批结构校验（Now 非负、kind 合法、ID 字符集与字节上限、ReadyAt 非负），通过后才读取状态；再检查时间单调性（`ErrTime`）。
- 校验通过后以“候选事务”方式顺序执行 Enqueue/Cancel：Enqueue 分配递增 revision，Cancel 删除条目；每一步记录 undo 日志（被覆盖/删除的旧值及 revision 基数）。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，按逆序回放 undo 日志，并回滚 `nextRevision` 与 `now`，保证失败批次对状态、时间、revision 完全无副作用。
- 成功批次：`now` 前进，非空批次 generation 恰好加一，空批次只前进时间不增 generation。

### 所有权与并发
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，调用间线性化，支持任意并发调用。
- `Item` 为值类型；`Pop` 与 `Snapshot` 返回的切片均为新建拷贝，调用方修改返回值不影响队列内部状态。
- `Queue` 内部状态不外泄，无回调、无 goroutine，锁内不做阻塞操作。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个操作）：结构校验 O(k·L)（L 为 ID 长度），执行 O(k)，末尾容量检查 O(1)。
- `Pop`（n 个条目，取 m 个）：候选收集 O(n)，排序 O(n log n)，删除 O(m)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
