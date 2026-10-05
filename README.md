# resourcecatalog091

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 为主索引，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节）、`generation`、`revision` 三个计数器。所有共享状态由一把 `sync.Mutex` 保护，公开方法（`Apply`/`Get`/`Snapshot`）均可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 结构校验：先对全部 Op 做纯结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；
2. 候选执行：克隆当前 `records` 与计数器为候选状态，按输入顺序应用 Put/Delete，Put 分配连续 revision，Delete 不分配且不存在的名称返回 `ErrNotFound`；
3. 末段容量检查与提交：仅在批次末检查记录数与 Value 总字节上限，超限返回 `ErrCapacity`。任何失败直接丢弃候选状态，已提交的 `records`、`generation`、`revision` 均不变，实现原子回滚。非空成功批次 `generation` 只加一，空批次不变。

**所有权**：Put 的 Value 在写入时深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方对返回切片的修改不影响内部状态，内部状态也不受调用方后续修改影响。`Snapshot` 记录按名称排序，`Changed` 按名称排序且每个名称只保留批次内最终版本。

**复杂度**：设批次含 n 个 Op、目录含 m 条记录。`Apply` 为 O(m + n)（克隆候选 + 顺序应用），排序 `Changed` 为 O(k log k)，k 为变更名称数；`Get` 为 O(1)（不计拷贝）；`Snapshot` 为 O(m log m)。锁持有期间无 IO，读操作与写操作互斥但临界区均为内存操作。
