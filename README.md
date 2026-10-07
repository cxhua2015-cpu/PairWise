# metacatalog381

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节数）运行合计，避免容量检查时全表扫描。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，排序成本为 O(k log k)（k 为涉及记录数）。

### 候选事务（candidate transaction）
- `Apply` 分三个阶段：**完整结构校验**（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value，全部在任何状态读取之前完成）→ **在索引副本上按输入顺序执行 Put/Delete**（Put 分配连续 revision，Delete 不分配；Delete 缺失记录即 `ErrNotFound`）→ **批次末容量检查**（记录数与 Value 总字节，超限返回 `ErrCapacity`）。
- 任何失败都直接丢弃候选副本，已提交的索引、generation 与 revision 计数器完全不受影响，实现原子回滚；成功时一次性换入候选副本，非空批次 generation 恰好 +1。

### 所有权
- Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，调用方对返回切片的修改不会污染内部状态，反之亦然。
- 存储中的 Value 永不原地修改，因此候选副本与已提交索引可以安全共享底层字节切片（写时复制）。

### 并发
- 所有公开方法通过单一 `sync.Mutex` 串行化，批次原子生效；`Get`/`Snapshot` 同样持锁，保证读到一致的已提交状态。

### 复杂度
- `Apply`：O(n·m + k log k)，n 为批次 op 数，m 为 Value 平均字节数（深拷贝），k 为变更记录数；候选索引复制为 O(R)，R 为当前记录数。
- `Get`：O(v)，v 为 Value 字节数（深拷贝）；`Snapshot`：O(R·v + R log R)。
- 空间：O(R·v)，另加 Apply 期间的候选副本 O(R) 指针开销。
