# resourceledger152

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按名称 O(1) 定位账户。
- 不维护有序索引：`Top` 与 `Snapshot` 在读取时即时收集并排序，避免写路径上的额外维护成本。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与长度、未使用字段必须为零、绝对值上限），不读取任何状态。
- 随后在写锁内把账户表浅拷贝为候选 map，按输入顺序执行 Add/Set/Delete；Add/Set 从候选的 `nextRev` 起分配连续 revision。
- Add 在算术前检测 int64 溢出，并对运算结果执行绝对值上限检查；任何失败直接返回，候选被丢弃，实现整体回滚。
- 账户容量上限仅在批次末对候选表检查一次（允许批内先删后建）。全部通过后一次性提交：替换 map、推进 `nextRev`，非空批次 generation 恰好加一。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 取写锁，`Top`/`Snapshot` 取读锁（`sync.RWMutex`）。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为每次调用新建，`Account` 为值类型，调用方修改返回值不影响内部状态。

**复杂度**
- `Apply`：O(B + A)，B 为批内 op 数，A 为当前账户数（候选拷贝）；校验与溢出检查均为 O(1)/op。
- `Top(n)`：O(A log A) 排序后取前 n；`Snapshot`：O(A log A) 按名称排序。
- 空间：O(A)，候选事务期间临时 O(A)。
