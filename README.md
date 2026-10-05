# taskqueue115

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序，
  不维护持久堆结构；队列容量受 `MaxItems` 约束，排序开销可控。

### 候选事务
- `Apply` 先做完整结构校验（时间非负、kind 合法、ID 字符集与长度），不读取任何状态；
  之后做单调时间检查（`Now < now` 返回 `ErrTime`）。
- 随后在克隆的候选 map 上顺序执行 Enqueue/Cancel：Enqueue 分配单调递增的 revision，
  Cancel 删除条目；最终容量只在所有操作完成后检查。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，
  时间、条目与 revision 计数器全部保持原值，实现天然回滚；成功则整体提交，
  非空批次 generation 恰好加一，空批次不变。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把互斥锁保护，可并发调用。
- `Pop` 在选择后原子删除并推进时间；`Snapshot` 与 `Pop` 返回的切片均为新建副本，
  调用方修改不会影响内部状态。

### 复杂度（n = 当前条目数，k = 批次操作数，m = 弹出上限）
- `Apply`：O(n + k)（克隆候选 + 顺序执行），校验 O(k)。
- `Pop`：O(n log n) 排序 + O(m) 选取与删除。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
