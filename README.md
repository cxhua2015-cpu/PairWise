# metacatalog396

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护
`totalBytes`、`generation`、`revision` 三个计数器。`Snapshot` 与 `Result.Changed`
在返回前按名称排序，不额外维护有序结构。

**候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value
长度），不触碰任何状态。随后在不修改主索引的前提下，用 `touched` 映射缓存被波及
记录的原始值，并在候选计数器（`candBytes`、`candRev`）上按输入顺序模拟 Put/Delete。
记录数与 Value 总字节容量只在批次末检查；任何失败（`ErrNotFound`、`ErrCapacity`）
直接返回，主索引、generation、revision 均未改动，天然回滚。全部通过后才把候选
变更一次性提交。

**所有权**：Put 的 Value 在提交时深拷贝；`Get`、`Snapshot`、`Result.Changed` 返回的
Value 与记录切片均为新分配的副本，调用方对返回数据的修改不会影响内部状态，反之
亦然。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 持写锁，`Get`/`Snapshot` 持读锁，
可并行执行。revision 在写锁内单调递增分配，批次间不会重复。

**复杂度**：结构校验 O(批次总字节)；候选模拟 O(k)，k 为批次数；提交 O(t)，t 为
被触及的不同名称数；`Changed` 排序 O(t log t)。`Get` O(1)（含返回值拷贝 O(|v|)）；
`Snapshot` O(n log n)，n 为记录数。空间 O(n + t)。
