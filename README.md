# eventqueue

并发安全的内存型“事件优先队列”，语义见 `SPEC.md`。Go 1.22+，仅标准库。

## 索引

- 主索引为 `map[string]Item`：按 ID O(1) 定位，用于 Enqueue 的 `ErrExists` 判重与 Cancel 删除。
- 弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，Pop/Snapshot 时现取现排；队列规模受 `MaxItems` 上限约束，排序开销可控。

## 候选事务

`Apply` 先在主索引的副本（候选 map）上顺序执行全部 Ops，并携带候选的 `nextRevision`/`lastRevision`；任一 Op 失败或最终容量超限即整体丢弃候选，队列的时间、状态、revision 与 generation 完全不变（回滚）。只有全部成功且 `len(candidate) <= MaxItems` 时才一次性提交，因此容量检查天然“只在末尾进行”，同批次内先 Cancel 再 Enqueue 可以越过容量限制。

## 所有权

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由单一 `sync.Mutex` 串行化，可并发调用；`Apply` 与 `Pop` 的原子性由互斥锁保证。
- `Snapshot.Items` 与 `Pop` 返回值均为新分配的切片与 `Item` 值拷贝，调用方修改不影响内部状态；`Item` 不含引用字段，值拷贝即深拷贝。
- 时间单调非减：`now < 0` 为 `ErrInvalidInput`，小于当前时间为 `ErrTime`，且仅在操作成功时推进。

## 复杂度

设 n 为当前元素数，k 为批次 Op 数，m 为 Pop 的 limit：

- `Apply`：结构校验 O(k)，候选复制 O(n)，执行 O(k)，合计 O(n + k) 时间、O(n) 额外空间。
- `Pop`：筛选 O(n)，排序 O(n log n)，删除 O(m)，合计 O(n log n)。
- `Snapshot`：O(n log n)（复制 + 规范序排序）。
- 空间：O(n)。
