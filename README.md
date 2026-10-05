# resourcecatalog101

并发安全的内存型资源目录，语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，键即资源名称，O(1) 定位。
- 另维护 `totalBytes`（Value 总字节）与 `nextRevision` 计数器，避免容量检查与 revision 分配时遍历全表。
- 无二级索引；`Snapshot`/`Result.Changed` 的名称排序在读取时按需进行。

### 候选事务（candidate transaction）
`Apply` 分三个阶段，全程持有写锁：
1. **结构校验**：在读取任何状态前校验全部 Op 的 kind、名称字符集/长度、Value 长度，失败返回 `ErrInvalidInput`。
2. **候选执行**：克隆当前记录 map 得到候选状态，按输入顺序执行 Put/Delete；Put 从单调递增的 `nextRevision` 分配连续 revision 并深拷贝 Value，Delete 不分配 revision，删除不存在的名称返回 `ErrNotFound`。
3. **提交或回滚**：仅在批次末检查记录数与 Value 总字节容量（`ErrCapacity`）。任一阶段失败时直接丢弃候选状态，`generation` 与 `nextRevision` 保持原值，实现零成本回滚；成功时整体换入候选状态，`generation` 只增加一次。

### 所有权
- 入参 `Op.Value` 在 Put 时深拷贝，调用方后续修改不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 均为深拷贝，返回切片与内部状态完全隔离。
- 记录中的 Value 一旦写入绝不原地修改，因此候选 map 可以与现役 map 安全共享底层字节切片。

### 并发
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁。
- 结构校验不依赖状态，在加锁前完成，缩短临界区。

### 复杂度
- `Apply`：时间 O(n + m log m)，n 为批次 Op 数，m 为触及的不同名称数；另加一次 O(r) 的 map 克隆（r 为当前记录数）。空间 O(r)。
- `Get`：O(1) 查询 + O(v) 拷贝（v 为 Value 长度）。
- `Snapshot`：O(r log r) 排序 + O(总字节数) 拷贝。
