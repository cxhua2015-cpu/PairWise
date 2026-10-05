# secretcatalog

并发安全的内存型密钥元数据目录。仅依赖标准库，Go 1.22+。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为名称，`entry` 持有 Value 副本与 revision。另维护 `total`（Value 总字节数）、`gen`（generation）与 `nextRev`（下一个待分配 revision，从 1 开始）。单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，因此所有公开方法可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 结构校验：在读取任何状态前校验整个批次的 kind、名称字符集与长度、Value 长度，失败返回 `ErrInvalidInput`。
2. 候选执行：克隆当前索引到候选 map，按输入顺序应用 Put/Delete；Put 分配连续 revision 并计入 `total`，Delete 不分配 revision，删除不存在的名称返回 `ErrNotFound`。
3. 末尾容量检查：仅此时校验最终记录数与 Value 总字节上限，超限返回 `ErrCapacity`。

任一阶段失败都直接丢弃候选，已提交状态、generation 与 revision 完全不变（回滚）。成功时一次性提交候选，非空批次 generation 恰好加一，空批次不改变任何状态。`Result.Changed` 按名称排序，每个被触碰的名称只出现一次，内容为该名称在批次末的最终状态（Delete 对应 `Value == nil`、`Revision == 0`）。

**所有权**：所有跨边界传递的 Value 都做深拷贝——Put 时拷入，`Get`/`Snapshot`/`Result.Changed` 拷出；调用方对返回切片的任何修改不影响内部状态，反之亦然。`Snapshot.Records` 按名称排序，且与内部状态完全隔离。

**复杂度**（n = 当前记录数，k = 批次内 op 数，B = 涉及 Value 总字节数）：
- `Apply`：时间 O(n + k + B + k log k)（克隆索引、执行、Changed 排序），空间 O(n + B)。
- `Get`：O(1) 均摊 + O(|value|) 拷贝。
- `Snapshot`：O(n log n + B)。
