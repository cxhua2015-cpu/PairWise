# resourcecatalog151

并发安全的内存型资源目录，原子批次执行 Put/Delete，仅依赖标准库（Go 1.22+）。

## 设计说明

### 索引
- 主索引为 `map[string]record`，按名称 O(1) 定位；`record` 持有 `value []byte` 与 `revision uint64`。
- 另维护 `totalBytes` 累计值，避免每次容量检查都遍历全表。
- `Snapshot`/`Result.Changed` 在返回前按名称排序（`sort.Slice`），不维护有序索引。

### 候选事务（candidate transaction）
- `Apply` 分三阶段：
  1. **结构校验**：对整个批次做纯校验（kind 合法、名称字符集/长度、Value 长度、Delete 不带 Value），不读取任何状态，失败返回 `ErrInvalidInput`。
  2. **候选执行**：在写锁内把当前 map 浅拷贝为候选表，按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配；Delete 缺失键返回 `ErrNotFound`。
  3. **批次末容量检查**：仅在结束时校验记录数与 Value 总字节，超限返回 `ErrCapacity`。
- 任何失败都直接丢弃候选表，已提交的 `records`、`generation`、`nextRevision`、`totalBytes` 完全不变，实现回滚。
- 成功时一次性提交：非空批次 `generation` 恰好 +1，空批次不变。

### 所有权
- 写入时拷贝调用方传入的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回深拷贝。
- 调用方修改自己的切片或返回值，均不影响内部状态，反之亦然。

### 并发
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁。
- 所有公开方法可并发调用；`go test -race` 通过。

### 复杂度
- `Apply`：校验 O(L)，执行 O(n + m)，其中 L 为批次总字节、n 为 op 数、m 为当前记录数（候选拷贝）；排序 Changed 为 O(k log k)，k 为涉及键数。
- `Get`：O(1) 均摊。`Snapshot`：O(m log m)。
- 空间：O(m + 批次大小)。
