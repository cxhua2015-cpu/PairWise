# eventqueue

并发安全的内存型“事件优先队列”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 `Enqueue` 去重与 `Cancel` 删除。
- 不维护有序堆：`Pop`/`Snapshot` 时对候选集按需排序（Priority 降序、ReadyAt 升序、ID 升序），
  以换取写入路径的极简与无堆修复开销。

### 候选事务
- `Apply` 先做完整结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态。
- 随后在克隆的 map 上顺序执行 Enqueue/Cancel 并分配 revision；容量只在末尾检查一次。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃克隆，时间、状态与
  revision 计数器天然回滚；成功才整体提交并将 generation 加一（空批次只推进时钟）。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 串行化，无锁内阻塞调用，线性一致。
- `Item` 为值类型；`Pop`/`Snapshot` 返回新建切片与值拷贝，调用方修改不影响内部状态。
- 时钟为显式非负单调时间：负值返回 `ErrInvalidInput`，回退返回 `ErrTime`，失败调用不移动时钟。

### 复杂度
- `Apply`：O(k·n) 克隆 + O(k) 执行（k 为批大小，n 为当前元素数；克隆为值拷贝）。
- `Pop`：O(n log n) 排序候选；`Snapshot`：O(n log n)。
- 空间：O(n)。
