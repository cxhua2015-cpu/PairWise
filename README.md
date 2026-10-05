# taskqueue090

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队、取消与删除。
- 不维护持久堆：`Pop`/`Snapshot` 时把候选收集到临时切片，按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序后返回。队列规模中等时，
  这比维护堆 + 懒删除更简单且无额外状态需要回滚。

## 候选事务

`Apply` 先在持锁状态下对整批 Op 做完整结构校验（kind、ID 字符集与字节
上限、ReadyAt 非负、时间非负且单调），不读取任何状态；校验通过后才顺序
执行 Enqueue/Cancel。每个变更记录一条 undo（新增记录删除即可，删除记录
保存旧值），任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败
（`ErrCapacity`）时逆序回放 undo，状态、`now`、`nextRevision` 与
`generation` 全部保持原值，实现原子回滚。容量只在批次末尾检查，因此
“先 Cancel 再 Enqueue” 的替换型批次可以成功。非空成功批次 `generation`
只加一次；空批次不改变任何状态。

## 所有权

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可并发调用。
- `Pop` 与 `Snapshot` 返回的 `[]Item` 都是新建切片并拷贝元素，调用方修改
  返回值不影响队列内部状态；`Item` 为纯值类型，无共享指针。

## 复杂度

设 n 为队列内任务数，b 为批次的 Op 数，k 为 Pop 的 limit：

- `New`：O(1)。
- `Apply`：O(b)（哈希表均摊），回滚同为 O(b)。
- `Pop`：O(n log n) 排序候选（就绪集为 r 时 O(r log r)），删除 O(k)。
- `Snapshot`：O(n log n)，返回有序副本。
- 空间：O(n)。
