# balanceledger327

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：唯一索引是 `map[string]Account`（按名称 O(1) 存取）。`Top` 与
  `Snapshot` 在读取时按需物化并排序，不维护有序结构——写路径因此保持 O(1)，
  读路径为 O(n log n)，在账户数受 `MaxAccounts` 上限约束时开销可控。
- **候选事务（candidate transaction）**：`Apply` 先对整个批次做纯结构校验
  （kind、名称字符集与长度），不触碰状态；随后在单个写临界区内把每个账户的
  变更累积到独立的候选副本（`pending` map）上，溢出、绝对值上限、`ErrNotFound`
  等检查全部作用于候选状态。只有全部操作成功且批次末容量检查通过时，才把候选
  副本一次性合并回主索引；任一步失败直接返回，主状态、generation 与 revision
  计数器完全不变，实现整体回滚。revision 在候选阶段本地递增，仅在提交时写回，
  因此失败批次不会消耗 revision。
- **所有权**：`Account` 为纯值类型，map 中存副本；`Top`/`Snapshot`/`Result.Changed`
  返回的切片均为新建并填充值拷贝，调用方修改返回值不会影响内部状态。批次末
  容量检查基于“主索引 + 候选增量”的最终账户数，与规范一致。
- **并发**：单个 `sync.Mutex` 保护全部可变状态。`Apply` 的结构校验在锁外完成，
  状态读取与提交在锁内原子执行；`Top`/`Snapshot` 持锁读取，保证与写操作线性一致。
- **复杂度**：设批次含 k 个操作、当前 n 个账户——`Apply` 为 O(k)（另加 O(k) 候选
  空间）；`Top(m)` 与 `Snapshot` 为 O(n log n) 时间、O(n) 空间；`New` 为 O(1)。
