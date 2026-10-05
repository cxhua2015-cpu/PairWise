# taskqueue120

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引结构

- `map[string]*Item`：按 ID 索引，Enqueue/Cancel 去重与查找为 O(1)。
- `container/heap` 最小堆（`*Item`）：按规范顺序排序——Priority 降序、ReadyAt 升序、ID 升序。堆顶即下一个候选任务，`Pop` 只取出 `ReadyAt <= now` 的堆顶元素。
- 条目入队后不可变，map 与堆共享同一指针，无同步副本问题。

## 候选事务

`Apply` 先完整结构校验（kind、ID 字符集与字节上限、非负时间），再校验单调时间，然后在克隆的 map/堆上顺序执行 Enqueue/Cancel（Enqueue 分配递增 revision），最后才检查容量。任一失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选状态，时间、条目与 revision 计数全部天然回滚；成功才一次性提交并将 generation 加一。空批次为完全无操作。

## 所有权

- 所有公开方法持有同一把 `sync.Mutex`，可并发调用。
- `Pop`/`Snapshot` 返回的切片与 `Item` 值均为拷贝，调用方修改不影响内部状态。
- 时间为显式非负单调值：`now` 回退返回 `ErrTime`，负数或非法 limit 返回 `ErrInvalidInput`。

## 复杂度

- `Apply`：校验 O(k)，执行 O(k·log n)，候选克隆 O(n)（k 为批大小，n 为队列长度）。
- `Pop`：O(m·log n)（m 为实际弹出数）。
- `Snapshot`：O(n·log n)（拷贝并按规范顺序排序）。
- 空间：O(n)。
