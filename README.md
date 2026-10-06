# balanceledger202

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位。
- `Top` 与 `Snapshot` 不维护持久有序索引，而是在读取时复制全部账户并排序（`Top` 按值降序、名称升序；`Snapshot` 按名称升序）。账户数受 `MaxAccounts` 上限约束，读时排序成本可控，且避免写路径维护额外结构。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind 合法、名称字符集与长度），不读取任何状态。
- 然后在互斥锁内克隆当前账户表作为候选状态，按输入顺序在其上执行 Add/Set/Delete：Add 在算术前检测 int64 溢出并检查绝对值上限；Set 直接检查绝对值上限；Delete 要求账户存在。Add/Set 各自分配连续 revision。
- 最终账户容量（`MaxAccounts`）仅在批次末尾对候选状态检查。
- 任一步失败直接丢弃候选状态，账本保持原样（整体回滚）；全部成功才一次性提交，非空批次 generation 恰好加一。

**所有权**
- 所有公开方法通过 `sync.RWMutex` 保护，可并发调用；`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，与内部 map 中的值无共享，调用方修改不影响账本。

**复杂度**（n = 账户数，b = 批次数）
- `Apply`：时间 O(n + b)，空间 O(n)（候选克隆）。
- `Top`：O(n log n)，空间 O(n)。
- `Snapshot`：O(n log n)，空间 O(n)。
