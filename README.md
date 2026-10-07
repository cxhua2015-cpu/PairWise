# balanceledger362

并发安全的内存型余额账本，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

- **索引**：账户主索引为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot`
  在读取时对账户快照切片排序，不维护额外的有序结构，写入路径保持极简。
- **候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰任何状态；
  随后在账户 map 的私有副本（候选事务）上按输入顺序执行 Add/Set/Delete，
  算术前检测 int64 溢出并执行绝对值上限，批次末才检查账户容量。任一步失败直接丢弃
  候选副本，实现整体回滚；全部成功才一次性替换内部 map 并推进 generation 与 revision。
- **所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，调用方修改
  返回值不会影响账本内部状态；内部状态只通过 `Apply` 的提交路径整体替换。
- **并发**：所有公开方法由单个 `sync.Mutex` 保护；`Options` 在 `New` 后不可变，
  结构校验阶段无需持锁。
- **复杂度**：`Apply` 为 O(k + n)，k 为批内 op 数、n 为账户数（候选副本克隆）；
  `Top` 为 O(n log n)；`Snapshot` 为 O(n log n)；单次 Add/Set/Delete 的索引操作为 O(1)。
