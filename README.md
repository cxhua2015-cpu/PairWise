# metacatalog316

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 为主索引，按名称 O(1) 定位记录；另维护
`totalValue`（Value 总字节数）、`generation` 与 `nextRevision` 计数器。`Snapshot`
与 `Result.Changed` 在返回前对名称排序，因此除排序输出外无有序索引结构。

**候选事务**：`Apply` 先在候选副本（记录 map 的浅拷贝 + 计数器副本）上按输入顺序
执行全部 Put/Delete；Put 分配连续 revision，Delete 不分配。任何失败（结构校验、
`ErrNotFound`、批次末容量检查）直接丢弃候选，状态、generation、revision 均不外泄；
成功时一次性提交候选，非空批次 generation 恰好 +1。

**所有权**：Put 的 Value 在提交前深拷贝；`Get`/`Snapshot` 返回的 Value 也是深拷贝，
调用方对返回切片的修改不影响内部状态，反之亦然。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Get`/`Snapshot` 取读锁，
所有公开方法可安全并发调用。

**复杂度**：`Apply` 为 O(n + m)（n 为批内 op 数，m 为当前记录数，用于候选拷贝），
另加 O(c log c) 的 Changed 排序（c 为变更名数）；`Get` 为 O(1)（不计返回值拷贝）；
`Snapshot` 为 O(m log m)。候选拷贝可进一步优化为写时复制，但当前规模下简单拷贝
已足够且语义清晰。
