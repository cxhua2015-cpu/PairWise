# dispatchbox

并发安全的内存型调度任务箱（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、Enqueue 去重与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时把 map 物化为切片并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，保证顺序确定且实现简单。

### 候选事务
- `Apply` 先做整批结构校验（时间非负、Kind 合法、ID 字符集与字节上限、ReadyAt 非负），再检查时间单调性，均不触碰状态。
- 随后在候选副本（克隆的 map + 候选 nextRevision）上顺序执行 Enqueue/Cancel；任一步失败（ErrExists/ErrNotFound）或末尾容量检查失败（ErrCapacity）直接丢弃候选，时间、状态、revision 全部自然回滚。
- 全部成功才一次性提交：替换 map、推进时间、提交 nextRevision；非空批次 generation 恰好加一，空批次不变。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用；`Pop` 的选择与删除在同一临界区内原子完成。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，`Item` 为纯值类型，调用方修改返回值不影响内部状态。

### 复杂度
- `Apply`：O(n + k)，n 为当前任务数（克隆候选），k 为批内操作数。
- `Pop` / `Snapshot`：O(n log n)（排序），Pop 另加 O(limit) 次 map 删除。
- 空间：O(n)。
