# metacatalog376

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 为主索引，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节）、`generation`、`revision` 三个计数器。单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，因此所有公开方法可并发调用。

**候选事务**：`Apply` 分两阶段。第一阶段在不持锁的情况下做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），任何失败直接返回 `ErrInvalidInput`，不读取状态。第二阶段持写锁，把当前记录复制到候选 map 上按输入顺序执行 Put/Delete：Put 递增 revision 并写入候选，Delete 校验存在性（缺失返回 `ErrNotFound`）且不分配 revision。批次末才检查记录数与 Value 总字节容量（`ErrCapacity`）。任一失败直接丢弃候选，内部 map、generation、revision 均未被修改，实现天然回滚；全部成功才整体提交，非空批次 generation 恰好加一。

**所有权**：Put 的 Value 在写入前深拷贝，调用方之后修改入参切片不影响目录；`Get`、`Snapshot`、`Result.Changed` 返回的 Value 同样是深拷贝，调用方修改返回值不会污染内部状态。`Snapshot` 与 `Changed` 均按名称排序，返回切片与内部状态完全隔离。

**复杂度**：结构校验 O(批次总字节)；候选事务 O(已有记录数 + 批次大小)（复制 map 加逐 op 的 O(1) map 操作）；`Get` O(名称长度 + Value 长度)（深拷贝）；`Snapshot` O(n log n)（n 为记录数，排序主导）。空间 O(记录数 × 平均 Value 大小)。
