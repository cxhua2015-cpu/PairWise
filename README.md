# metacatalog346

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，值为
`{value, revision}`；另维护 `totalValue`（Value 总字节数的滚动计数）、
`generation` 与 `nextRevision`。所有共享状态由一把 `sync.RWMutex` 保护：
`Apply` 持写锁，`Get`/`Snapshot` 持读锁，因此所有公开方法均可并发调用。

**候选事务**：`Apply` 先在持锁状态下对整个批次做完整结构校验（kind、名称
字符集与长度、Value 长度），不读取任何状态；随后把当前 map 与计数器复制为
一份候选状态，按输入顺序在其上执行 Put/Delete。Put 从单调递增的
`nextRevision` 分配连续 revision（覆盖同名记录也会分配新 revision），Delete
不分配 revision，删除不存在的名字返回 `ErrNotFound`。记录数与 Value 总字节
容量只在批次末检查，超出返回 `ErrCapacity`。任一步失败直接丢弃候选状态，
已提交状态、generation 和 revision 完全不变（天然回滚）；成功时才一次性
换入候选状态并将 generation 加一（空批次不增加）。

**所有权**：Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed`
返回的 Value 与切片均为独立副本，调用方后续修改不会影响目录内部状态，
目录后续变更也不会影响已返回的快照。`Snapshot` 与 `Changed` 均按名称排序。

**复杂度**：结构校验 O(批次总字节数)；候选复制 O(记录数)；批次执行
O(操作数)；`Get` O(1)；`Snapshot` 与 `Changed` 排序为 O(n log n)。
