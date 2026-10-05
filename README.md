# resourcecatalog116

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- `Snapshot` 与 `Result.Changed` 在返回前对名称排序（`sort.Strings`），保证确定性输出。
- 单调计数器：`generation`（每个非空成功批次 +1）与 `nextRevision`（仅 Put 分配，Delete 不分配）。

### 候选事务（candidate transaction）
`Apply` 分三个阶段，全程持有写锁：
1. **结构校验**：先对整个批次做纯结构校验（kind 合法、名称字符集/长度、Value 长度），不读取任何状态；失败返回 `ErrInvalidInput`。
2. **候选执行**：克隆当前索引得到候选 map，按输入顺序在其上执行 Put/Delete；Put 深拷贝 Value 并分配连续 revision，Delete 缺失记录即返回 `ErrNotFound`。
3. **提交前容量检查**：记录数与 Value 总字节容量只在批次末对候选结果检查，超限返回 `ErrCapacity`。

任一阶段失败直接丢弃候选 map，`generation` 与 `nextRevision` 均未提交，实现整体回滚；成功则一次性交换索引并推进计数器。

### 所有权
- Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离，调用方可自由修改。

### 并发
- 单把 `sync.RWMutex`：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，所有公开方法可并发调用（`-race` 通过）。

### 复杂度
- `Apply`：O(n + m log m + R)，n 为批次 op 数，m 为涉及名称数（排序），R 为当前记录数（克隆候选索引与容量合计）。
- `Get`：O(1) 加 O(v) 拷贝；`Snapshot`：O(R log R)。
