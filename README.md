# taskqueue200

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 核心语义

- 显式非负单调时间：`Batch.Now` 必须 ≥ 当前队列时间，否则 `ErrTime`；成功批次提交时间，失败批次回滚时间、状态与 revision。
- `Apply` 原子顺序执行 `Enqueue`/`Cancel`：先做完整结构校验（kind、ID 字符集与字节上限、非负时间），再读取状态；任一操作失败整批回滚。
- `Enqueue` 分配单调递增 revision；容量只在批次末尾检查（允许批内先超后降）。
- 非空成功批次 generation 恰好 +1；空批次 generation 不变。
- `Pop(now, limit)` 选取 `ReadyAt <= now` 的任务，按 Priority 降序、ReadyAt 升序、ID 升序返回并原子删除。
- 所有公开方法并发安全；`Snapshot`/`Pop` 返回的切片与内部状态完全隔离。

## 索引结构

- `items []Item`：始终保持规范顺序（Priority 降序、ReadyAt 升序、ID 升序）的有序切片。
- `index map[string]int`：ID → `items` 下标，用于 O(1) 存在性判断与 Cancel 定位。

## 候选事务（candidate transaction）

`Apply` 在持锁后克隆 `items`、`index` 与 `nextRevision` 为候选状态，全部操作作用于候选；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，原状态、时间与 revision 天然不变；全部成功且末尾容量检查通过后才整体提交，generation 恰增一次。

## 所有权

- 队列内部状态仅由互斥锁保护的临界区访问。
- `Snapshot.Items` 与 `Pop` 返回值均为新分配的副本，调用方修改不影响队列。
- 批次中的 ID 字符串按值拷贝进 `Item`，不保留调用方可变引用。

## 复杂度

- `Apply`：结构校验 O(L)（L 为批内字节数）；每个 Enqueue 二分定位 O(log n) + 插入搬移 O(n)，Cancel O(n)；候选克隆 O(n)。整批 O(n + k·n)。
- `Pop`：O(n) 扫描筛选 + O(n) 重建索引。
- `Snapshot`：O(n) 拷贝。
- 空间：O(n)。
