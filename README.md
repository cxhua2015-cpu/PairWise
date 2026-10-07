# readyqueue335

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

- **主索引**：`map[string]Item`，以 ID 为键，Enqueue/Cancel/去重均为 O(1)。
- **就绪视图**：不维护持久堆；`Pop`/`Snapshot` 时从主索引收集条目并按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。条目数受 `MaxItems` 上限约束，
  因此单次排序成本有界，换来实现与回滚的简单性。

## 候选事务

`Apply` 是一个候选事务：先在持锁状态下对整批 Op 做完整结构校验（kind、ID 字符集与
字节上限），不做任何状态读取；校验通过后按顺序执行 Enqueue/Cancel，Enqueue 从单调
计数器分配 revision，同时把逆操作记入撤销日志。任一步失败（`ErrExists`/`ErrNotFound`）
或最终容量检查失败（`ErrCapacity`）时，按撤销日志逆序回滚条目并还原 revision 计数器，
时间与 generation 只在成功提交时推进，因此失败批次对外完全不可见。非空成功批次
generation 恰好加一；空批次只推进时间。

## 所有权

- 队列内部独占所有 `Item`；`Pop` 在返回前原子地从主索引删除所选项。
- `Pop`/`Snapshot` 返回的切片与内部状态完全隔离，调用方可自由修改。
- 所有公开方法共用一把互斥锁，可任意并发调用；时间在所有入口（Apply/Pop）统一做
  非负与单调检查，失败路径不产生任何副作用。

## 复杂度

设 n 为当前条目数、b 为批次 Op 数：

- `New`：O(1)。
- `Apply`：O(b) 校验与执行，回滚 O(b)，最终容量检查 O(1)。
- `Pop`：O(n log n)（收集就绪项并排序，取前 limit 个删除）。
- `Snapshot`：O(n log n)（拷贝并排序）。
